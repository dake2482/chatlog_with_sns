package v4

import (
	"testing"

	"github.com/sjzar/chatlog/internal/model"
)

func TestMatchesSNSUsernameFilter(t *testing.T) {
	post := &model.SNSPost{NickName: "HEXIN"}

	if !matchesSNSUsernameFilter("HEXIN", "wxid_9735367353412", post) {
		t.Fatal("expected nickname filter to match")
	}

	if !matchesSNSUsernameFilter("wxid_9735367353412", "wxid_9735367353412", post) {
		t.Fatal("expected username filter to match")
	}

	if matchesSNSUsernameFilter("OTHER", "wxid_9735367353412", post) {
		t.Fatal("expected unrelated filter not to match")
	}
}
