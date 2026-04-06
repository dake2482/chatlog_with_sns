package model

import "testing"

func TestParseSNSContentArticleAndEscapedURL(t *testing.T) {
	xmlContent := `<TimelineObject><username>wxid_test</username><nickname>张三</nickname><createTime>1712345678</createTime><contentDesc>测试文章</contentDesc><type>3</type><title>标题</title><description>简介</description><contentUrl>https://example.com?a=1&amp;b=2</contentUrl></TimelineObject>`

	post, err := ParseSNSContent(xmlContent)
	if err != nil {
		t.Fatalf("ParseSNSContent returned error: %v", err)
	}
	if post.ContentType != "article" {
		t.Fatalf("expected article content type, got %q", post.ContentType)
	}
	if post.Article == nil {
		t.Fatal("expected article data, got nil")
	}
	if post.Article.URL != "https://example.com?a=1&b=2" {
		t.Fatalf("expected unescaped url, got %q", post.Article.URL)
	}
	if post.NickName != "张三" {
		t.Fatalf("expected nickname 张三, got %q", post.NickName)
	}
}
