package filters

import (
	"sync"
	"testing"
)

func TestIsMuted_GroupBlocking(t *testing.T) {
	f := New(true, map[string]bool{})
	if !f.IsMuted("557192682188-1629234117@g.us") {
		t.Error("group JID should be blocked when muteGroups=true")
	}
	if f.IsMuted("557192669940@s.whatsapp.net") {
		t.Error("DM JID should not be blocked when only groups are blocked")
	}
}

func TestIsMuted_GroupsAllowed(t *testing.T) {
	f := New(false, map[string]bool{})
	if f.IsMuted("557192682188-1629234117@g.us") {
		t.Error("group JID should not be blocked when muteGroups=false")
	}
}

func TestIsMuted_ExplicitJID(t *testing.T) {
	f := New(false, map[string]bool{"557192669940@s.whatsapp.net": true})
	if !f.IsMuted("557192669940@s.whatsapp.net") {
		t.Error("explicitly blocked JID should be blocked")
	}
	if f.IsMuted("557192735591@s.whatsapp.net") {
		t.Error("non-blocked JID should not be blocked")
	}
}

func TestMuteUnmute(t *testing.T) {
	f := New(false, map[string]bool{})
	jid := "557192669940@s.whatsapp.net"

	f.Mute(jid)
	if !f.IsMuted(jid) {
		t.Error("JID should be muted after Mute()")
	}
	if len(f.ListMuted()) != 1 {
		t.Errorf("expected 1 muted, got %d", len(f.ListMuted()))
	}

	f.Unmute(jid)
	if f.IsMuted(jid) {
		t.Error("JID should not be muted after Unmute()")
	}
}

func TestSetMuteGroups(t *testing.T) {
	f := New(false, map[string]bool{})
	group := "557192682188-1629234117@g.us"

	f.SetMuteGroups(true)
	if !f.IsMuted(group) {
		t.Error("groups should be blocked after SetMuteGroups(true)")
	}
	if !f.MuteGroups() {
		t.Error("MuteGroups() should return true")
	}

	f.SetMuteGroups(false)
	if f.IsMuted(group) {
		t.Error("groups should not be blocked after SetMuteGroups(false)")
	}
}

func TestConcurrentAccess(t *testing.T) {
	f := New(true, map[string]bool{})
	var wg sync.WaitGroup
	jid := "557192669940@s.whatsapp.net"

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); f.IsMuted(jid) }()
		go func() { defer wg.Done(); f.Mute(jid) }()
		go func() { defer wg.Done(); f.Unmute(jid) }()
	}
	wg.Wait()
}

func TestFeatureToggles_Defaults(t *testing.T) {
	f := New(false, map[string]bool{})
	if !f.ReplyDrafts() {
		t.Error("reply drafts should be on by default")
	}
	if !f.TranslateAudio() {
		t.Error("audio translation should be on by default")
	}
	if !f.TranslateText() {
		t.Error("text translation should be on by default")
	}
}

func TestFeatureToggles_Set(t *testing.T) {
	f := New(false, map[string]bool{})

	f.SetReplyDrafts(false)
	if f.ReplyDrafts() {
		t.Error("reply drafts should be off")
	}

	f.SetTranslateAudio(false)
	if f.TranslateAudio() {
		t.Error("audio translation should be off")
	}

	f.SetTranslateText(false)
	if f.TranslateText() {
		t.Error("text translation should be off")
	}
}
