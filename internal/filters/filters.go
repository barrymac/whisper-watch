package filters

import (
	"strings"
	"sync"
)

type Filters struct {
	mu sync.RWMutex

	muteGroups bool
	mutedJIDs  map[string]bool

	replyDrafts    bool
	translateAudio bool
	translateText  bool
	ollamaModel    string
}

func New(muteGroups bool, mutedJIDs map[string]bool, ollamaModel string) *Filters {
	return &Filters{
		muteGroups:    muteGroups,
		mutedJIDs:     mutedJIDs,
		replyDrafts:   true,
		translateAudio: true,
		translateText:  true,
		ollamaModel:   ollamaModel,
	}
}

func (f *Filters) IsMuted(jid string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.muteGroups && strings.HasSuffix(jid, "@g.us") {
		return true
	}
	return f.mutedJIDs[jid]
}

func (f *Filters) MuteGroups() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.muteGroups
}

func (f *Filters) SetMuteGroups(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.muteGroups = v
}

func (f *Filters) Mute(jid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutedJIDs[jid] = true
}

func (f *Filters) Unmute(jid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.mutedJIDs, jid)
}

func (f *Filters) ListMuted() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]string, 0, len(f.mutedJIDs))
	for jid := range f.mutedJIDs {
		result = append(result, jid)
	}
	return result
}

func (f *Filters) ReplyDrafts() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.replyDrafts
}

func (f *Filters) SetReplyDrafts(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyDrafts = v
}

func (f *Filters) TranslateAudio() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.translateAudio
}

func (f *Filters) SetTranslateAudio(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.translateAudio = v
}

func (f *Filters) TranslateText() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.translateText
}

func (f *Filters) SetTranslateText(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.translateText = v
}

func (f *Filters) OllamaModel() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.ollamaModel
}

func (f *Filters) SetOllamaModel(model string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ollamaModel = model
}
