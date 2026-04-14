package model

import "testing"

func TestParseFavoriteContentArticle(t *testing.T) {
	xmlContent := `<favitem type="5"><link>https://example.com?a=1&amp;b=2</link><weburlitem><pagetitle>标题</pagetitle><pagedesc>简介</pagedesc></weburlitem></favitem>`

	content, err := ParseFavoriteContent(xmlContent, 5)
	if err != nil {
		t.Fatalf("ParseFavoriteContent returned error: %v", err)
	}
	if content.Summary != "标题 - 简介" {
		t.Fatalf("expected article summary, got %q", content.Summary)
	}
	if content.Link != "https://example.com?a=1&b=2" {
		t.Fatalf("expected unescaped url, got %q", content.Link)
	}
}

func TestParseFavoriteContentChatRecord(t *testing.T) {
	xmlContent := `<favitem type="14"><title>群聊的聊天记录</title><datalist count="2"><dataitem><datadesc>第一条</datadesc></dataitem><dataitem><datadesc>第二条</datadesc></dataitem></datalist></favitem>`

	content, err := ParseFavoriteContent(xmlContent, 14)
	if err != nil {
		t.Fatalf("ParseFavoriteContent returned error: %v", err)
	}
	if content.Summary != "群聊的聊天记录（2 条消息）" {
		t.Fatalf("expected chat summary, got %q", content.Summary)
	}
}

func TestParseFavoriteContentFinder(t *testing.T) {
	xmlContent := `<favitem type="20"><finderFeed><nickname>书与饭</nickname><desc>新视频</desc></finderFeed></favitem>`

	content, err := ParseFavoriteContent(xmlContent, 20)
	if err != nil {
		t.Fatalf("ParseFavoriteContent returned error: %v", err)
	}
	if content.Summary != "书与饭 新视频" {
		t.Fatalf("expected finder summary, got %q", content.Summary)
	}
}
