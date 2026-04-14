package http

import (
	"embed"
	"encoding/csv"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/sjzar/chatlog/internal/errors"
	"github.com/sjzar/chatlog/internal/model"
	"github.com/sjzar/chatlog/pkg/util"
	"github.com/sjzar/chatlog/pkg/util/dat2img"
	"github.com/sjzar/chatlog/pkg/util/silk"
)

// EFS holds embedded file system data for static assets.
//
//go:embed static
var EFS embed.FS

func (s *Service) initRouter() {
	s.initBaseRouter()
	s.initMediaRouter()
	s.initAPIRouter()
	s.initMCPRouter()
}

func (s *Service) initBaseRouter() {
	staticDir, _ := fs.Sub(EFS, "static")

	s.router.StaticFS("/static", http.FS(staticDir))
	s.router.StaticFileFS("/favicon.ico", "./favicon.ico", http.FS(staticDir))
	s.router.StaticFileFS("/", "./index.htm", http.FS(staticDir))

	s.router.GET("/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	s.router.NoRoute(s.NoRoute)
}

func (s *Service) initMediaRouter() {
	s.router.GET("/image/*key", func(c *gin.Context) { s.handleMedia(c, "image") })
	s.router.GET("/video/*key", func(c *gin.Context) { s.handleMedia(c, "video") })
	s.router.GET("/file/*key", func(c *gin.Context) { s.handleMedia(c, "file") })
	s.router.GET("/voice/*key", func(c *gin.Context) { s.handleMedia(c, "voice") })
	s.router.GET("/data/*path", s.handleMediaData)
}

func (s *Service) initAPIRouter() {
	api := s.router.Group("/api/v1", s.checkDBStateMiddleware())
	{
		api.GET("/chatlog", s.handleChatlog)
		api.GET("/contact", s.handleContacts)
		api.GET("/chatroom", s.handleChatRooms)
		api.GET("/session", s.handleSessions)
		api.GET("/favorites", s.handleFavorites)
		api.GET("/sns", s.handleSNS)
	}
}

func (s *Service) initMCPRouter() {
	s.router.Any("/mcp", func(c *gin.Context) {
		s.mcpStreamableServer.ServeHTTP(c.Writer, c.Request)
	})
	s.router.Any("/sse", func(c *gin.Context) {
		s.mcpSSEServer.ServeHTTP(c.Writer, c.Request)
	})
	s.router.Any("/message", func(c *gin.Context) {
		s.mcpSSEServer.ServeHTTP(c.Writer, c.Request)
	})
}

// NoRoute handles 404 Not Found errors. If the request URL starts with "/api"
// or "/static", it responds with a JSON error. Otherwise, it redirects to the root path.
func (s *Service) NoRoute(c *gin.Context) {
	path := c.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api"), strings.HasPrefix(path, "/static"):
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
	default:
		c.Header("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate, value")
		c.Redirect(http.StatusFound, "/")
	}
}

func (s *Service) handleChatlog(c *gin.Context) {

	q := struct {
		Time    string `form:"time"`
		Talker  string `form:"talker"`
		Sender  string `form:"sender"`
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	var err error
	start, end, ok := util.TimeRangeOf(q.Time)
	if !ok {
		errors.Err(c, errors.InvalidArg("time"))
	}
	if q.Limit < 0 {
		q.Limit = 0
	}

	if q.Offset < 0 {
		q.Offset = 0
	}

	messages, err := s.db.GetMessages(start, end, q.Talker, q.Sender, q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}

	switch strings.ToLower(q.Format) {
	case "csv":
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_%s_%s.csv", q.Talker, start.Format("2006-01-02"), end.Format("2006-01-02")))
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		csvWriter := csv.NewWriter(c.Writer)
		csvWriter.Write([]string{"Time", "SenderName", "Sender", "TalkerName", "Talker", "Content"})
		for _, m := range messages {
			csvWriter.Write(m.CSV(c.Request.Host))
		}
		csvWriter.Flush()
	case "json":
		// json
		c.JSON(http.StatusOK, messages)
	default:
		// plain text
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		for _, m := range messages {
			c.Writer.WriteString(m.PlainText(strings.Contains(q.Talker, ","), util.PerfectTimeFormat(start, end), c.Request.Host))
			c.Writer.WriteString("\n")
			c.Writer.Flush()
		}
	}
}

func (s *Service) handleContacts(c *gin.Context) {

	q := struct {
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	list, err := s.db.GetContacts(q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}

	format := strings.ToLower(q.Format)
	switch format {
	case "json":
		// json
		c.JSON(http.StatusOK, list)
	default:
		// csv
		if format == "csv" {
			// 浏览器访问时，会下载文件
			c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		} else {
			c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		c.Writer.WriteString("UserName,Alias,Remark,NickName\n")
		for _, contact := range list.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%s,%s,%s\n", contact.UserName, contact.Alias, contact.Remark, contact.NickName))
		}
		c.Writer.Flush()
	}
}

func (s *Service) handleChatRooms(c *gin.Context) {

	q := struct {
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	list, err := s.db.GetChatRooms(q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}
	format := strings.ToLower(q.Format)
	switch format {
	case "json":
		// json
		c.JSON(http.StatusOK, list)
	default:
		// csv
		if format == "csv" {
			// 浏览器访问时，会下载文件
			c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		} else {
			c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		c.Writer.WriteString("Name,Remark,NickName,Owner,UserCount\n")
		for _, chatRoom := range list.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%s,%s,%s,%d\n", chatRoom.Name, chatRoom.Remark, chatRoom.NickName, chatRoom.Owner, len(chatRoom.Users)))
		}
		c.Writer.Flush()
	}
}

func (s *Service) handleSessions(c *gin.Context) {

	q := struct {
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	sessions, err := s.db.GetSessions(q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}
	format := strings.ToLower(q.Format)
	switch format {
	case "csv":
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		c.Writer.WriteString("UserName,NOrder,NickName,Content,NTime\n")
		for _, session := range sessions.Items {
			c.Writer.WriteString(fmt.Sprintf("%s,%d,%s,%s,%s\n", session.UserName, session.NOrder, session.NickName, strings.ReplaceAll(session.Content, "\n", "\\n"), session.NTime))
		}
		c.Writer.Flush()
	case "json":
		// json
		c.JSON(http.StatusOK, sessions)
	default:
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()
		for _, session := range sessions.Items {
			c.Writer.WriteString(session.PlainText(120))
			c.Writer.WriteString("\n")
		}
		c.Writer.Flush()
	}
}

func (s *Service) handleSNS(c *gin.Context) {
	q := struct {
		Username string `form:"username"`
		Limit    int    `form:"limit"`
		Offset   int    `form:"offset"`
		Format   string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	if q.Limit < 0 {
		q.Limit = 0
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	data, err := s.db.GetSNSTimeline(q.Username, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}

	switch strings.ToLower(q.Format) {
	case "json":
		c.JSON(http.StatusOK, data)
	case "csv":
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Content-Disposition", "attachment; filename=sns_timeline.csv")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		csvWriter := csv.NewWriter(c.Writer)
		csvWriter.Write([]string{"TID", "UserName", "NickName", "CreateTime", "ContentType", "ContentDesc", "XMLContent"})
		for _, item := range data {
			csvWriter.Write([]string{
				fmt.Sprintf("%v", item["tid"]),
				fmt.Sprintf("%v", item["user_name"]),
				fmt.Sprintf("%v", item["nickname"]),
				fmt.Sprintf("%v", item["create_time_str"]),
				fmt.Sprintf("%v", item["content_type"]),
				fmt.Sprintf("%v", item["content_desc"]),
				fmt.Sprintf("%v", item["xml_content"]),
			})
		}
		csvWriter.Flush()
	case "raw":
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		for _, item := range data {
			if xmlContent, ok := item["xml_content"].(string); ok && xmlContent != "" {
				c.Writer.WriteString(xmlContent)
				c.Writer.WriteString("\n")
				c.Writer.WriteString(strings.Repeat("=", 80))
				c.Writer.WriteString("\n")
			}
		}
	default:
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		for _, item := range data {
			if createTimeStr, ok := item["create_time_str"].(string); ok && createTimeStr != "" {
				c.Writer.WriteString(fmt.Sprintf("📅 %s\n", createTimeStr))
			}
			if nickname, ok := item["nickname"].(string); ok && nickname != "" {
				c.Writer.WriteString(fmt.Sprintf("👤 %s\n", nickname))
			}
			if contentDesc, ok := item["content_desc"].(string); ok && contentDesc != "" {
				c.Writer.WriteString(fmt.Sprintf("💬 %s\n", contentDesc))
			}
			if location, ok := item["location"].(*model.SNSLocation); ok && location != nil {
				c.Writer.WriteString("📍 ")
				if location.POIName != "" {
					c.Writer.WriteString(location.POIName)
					if location.POIAddress != "" {
						c.Writer.WriteString(fmt.Sprintf(" (%s)", location.POIAddress))
					}
				} else if location.City != "" {
					c.Writer.WriteString(location.City)
				}
				c.Writer.WriteString("\n")
			}
			switch item["content_type"] {
			case "image":
				if mediaList, ok := item["media_list"].([]model.SNSMedia); ok {
					c.Writer.WriteString(fmt.Sprintf("🖼️ 图片 (%d张)\n", len(mediaList)))
				}
			case "video":
				if mediaList, ok := item["media_list"].([]model.SNSMedia); ok && len(mediaList) > 0 {
					if mediaList[0].Duration != "" {
						c.Writer.WriteString(fmt.Sprintf("🎬 视频 (%s)\n", mediaList[0].Duration))
					} else {
						c.Writer.WriteString("🎬 视频\n")
					}
				}
			case "article":
				if article, ok := item["article"].(*model.SNSArticle); ok && article != nil {
					c.Writer.WriteString(fmt.Sprintf("📰 文章: %s\n", article.Title))
					if article.URL != "" {
						c.Writer.WriteString(fmt.Sprintf("   %s\n", article.URL))
					}
				}
			case "finder":
				if feed, ok := item["finder_feed"].(*model.SNSFinderFeed); ok && feed != nil {
					c.Writer.WriteString(fmt.Sprintf("📺 视频号: %s\n", feed.Nickname))
					if feed.Desc != "" {
						c.Writer.WriteString(fmt.Sprintf("   %s\n", feed.Desc))
					}
				}
			}
			c.Writer.WriteString(strings.Repeat("=", 80))
			c.Writer.WriteString("\n\n")
		}
	}
}

func (s *Service) handleFavorites(c *gin.Context) {
	q := struct {
		Type    string `form:"type"`
		Keyword string `form:"keyword"`
		Limit   int    `form:"limit"`
		Offset  int    `form:"offset"`
		Format  string `form:"format"`
	}{}

	if err := c.BindQuery(&q); err != nil {
		errors.Err(c, err)
		return
	}

	if q.Limit < 0 {
		q.Limit = 0
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	items, err := s.db.GetFavorites(q.Type, q.Keyword, q.Limit, q.Offset)
	if err != nil {
		errors.Err(c, err)
		return
	}

	switch strings.ToLower(q.Format) {
	case "json":
		c.JSON(http.StatusOK, items)
	case "csv":
		c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Writer.Header().Set("Content-Disposition", "attachment; filename=favorites.csv")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		csvWriter := csv.NewWriter(c.Writer)
		csvWriter.Write([]string{"ID", "TypeCode", "Type", "UpdateTime", "Summary", "Title", "Description", "Link", "FromUser", "From", "SourceChat", "SourceChatName"})
		for _, item := range items {
			csvWriter.Write([]string{
				fmt.Sprintf("%d", item.ID),
				fmt.Sprintf("%d", item.TypeCode),
				item.Type,
				item.UpdateTimeStr,
				item.Summary,
				item.Title,
				item.Description,
				item.Link,
				item.FromUser,
				item.FromName(),
				item.SourceChat,
				item.SourceChatName(),
			})
		}
		csvWriter.Flush()
	case "raw":
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		for _, item := range items {
			if item.XMLContent == "" {
				continue
			}
			c.Writer.WriteString(item.XMLContent)
			c.Writer.WriteString("\n")
			c.Writer.WriteString(strings.Repeat("=", 80))
			c.Writer.WriteString("\n")
		}
	default:
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Flush()

		for _, item := range items {
			c.Writer.WriteString(item.PlainText())
			c.Writer.WriteString("\n")
			c.Writer.WriteString(strings.Repeat("=", 80))
			c.Writer.WriteString("\n\n")
		}
	}
}

func (s *Service) handleMedia(c *gin.Context, _type string) {
	key := strings.TrimPrefix(c.Param("key"), "/")
	if key == "" {
		errors.Err(c, errors.InvalidArg(key))
		return
	}

	keys := util.Str2List(key, ",")
	if len(keys) == 0 {
		errors.Err(c, errors.InvalidArg(key))
		return
	}

	var _err error
	for _, k := range keys {
		if strings.Contains(k, "/") {
			if absolutePath, err := s.findPath(_type, k); err == nil {
				c.Redirect(http.StatusFound, "/data/"+absolutePath)
				return
			}
		}
		media, err := s.db.GetMedia(_type, k)
		if err != nil {
			_err = err
			continue
		}
		if c.Query("info") != "" {
			c.JSON(http.StatusOK, media)
			return
		}
		switch media.Type {
		case "voice":
			s.HandleVoice(c, media.Data)
			return
		default:
			c.Redirect(http.StatusFound, "/data/"+media.Path)
			return
		}
	}

	if _err != nil {
		errors.Err(c, _err)
		return
	}
}

func (s *Service) findPath(_type string, key string) (string, error) {
	absolutePath := filepath.Join(s.conf.GetDataDir(), key)
	if _, err := os.Stat(absolutePath); err == nil {
		return key, nil
	}
	switch _type {
	case "image":
		for _, suffix := range []string{"_h.dat", ".dat", "_t.dat"} {
			if _, err := os.Stat(absolutePath + suffix); err == nil {
				return key + suffix, nil
			}
		}
	case "video":
		for _, suffix := range []string{".mp4", "_thumb.jpg"} {
			if _, err := os.Stat(absolutePath + suffix); err == nil {
				return key + suffix, nil
			}
		}
	}
	return "", errors.ErrMediaNotFound
}

func (s *Service) handleMediaData(c *gin.Context) {
	relativePath := filepath.Clean(c.Param("path"))

	absolutePath := filepath.Join(s.conf.GetDataDir(), relativePath)

	if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "File not found",
		})
		return
	}

	ext := strings.ToLower(filepath.Ext(absolutePath))
	switch {
	case ext == ".dat":
		s.HandleDatFile(c, absolutePath)
	default:
		// 直接返回文件
		c.File(absolutePath)
	}

}

func (s *Service) HandleDatFile(c *gin.Context, path string) {

	b, err := os.ReadFile(path)
	if err != nil {
		errors.Err(c, err)
		return
	}
	out, ext, err := dat2img.Dat2Image(b)
	if err != nil {
		c.File(path)
		return
	}

	// Save decrypted file to local disk
	if s.conf.GetSaveDecryptedMedia() {
		s.saveDecryptedFile(path, out, ext)
	}

	switch ext {
	case "jpg", "jpeg":
		c.Data(http.StatusOK, "image/jpeg", out)
	case "png":
		c.Data(http.StatusOK, "image/png", out)
	case "gif":
		c.Data(http.StatusOK, "image/gif", out)
	case "bmp":
		c.Data(http.StatusOK, "image/bmp", out)
	case "mp4":
		c.Data(http.StatusOK, "video/mp4", out)
	default:
		c.Data(http.StatusOK, "image/jpg", out)
		// c.File(path)
	}
}

func (s *Service) HandleVoice(c *gin.Context, data []byte) {
	out, err := silk.Silk2MP3(data)
	if err != nil {
		c.Data(http.StatusOK, "audio/silk", data)
		return
	}
	c.Data(http.StatusOK, "audio/mp3", out)
}

// saveDecryptedFile saves the decrypted media file to local disk
func (s *Service) saveDecryptedFile(datPath string, data []byte, ext string) {
	// Generate target file path: replace .dat with actual extension
	outputPath := strings.TrimSuffix(datPath, filepath.Ext(datPath)) + "." + ext

	// Check if file already exists to avoid duplicate writes
	if _, err := os.Stat(outputPath); err == nil {
		return
	}

	// Write file
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Error().
			Err(err).
			Str("dat_path", datPath).
			Str("output_path", outputPath).
			Msg("Failed to save decrypted file")
		return
	}

	log.Debug().
		Str("dat_path", datPath).
		Str("output_path", outputPath).
		Str("format", ext).
		Int("size", len(data)).
		Msg("Decrypted file saved successfully")
}
