package bot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/barrymac/whisper-watch/internal/contacts"
	"github.com/barrymac/whisper-watch/internal/evolution"
	"github.com/barrymac/whisper-watch/internal/filters"
	"github.com/barrymac/whisper-watch/internal/ollama"
	"github.com/barrymac/whisper-watch/internal/state"
	"github.com/barrymac/whisper-watch/internal/whisper"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type TelegramBot struct {
	bot               *tgbot.Bot
	whisper           *whisper.Client
	chatID            int64
	filters           *filters.Filters
	store             *contacts.Store
	evolution         *evolution.Client
	ollama            *ollama.Client
	state             *state.Store
	ollamaConcurrency int
}

func New(token string, chatID int64, whisperClient *whisper.Client, f *filters.Filters, store *contacts.Store, evo *evolution.Client, ollamaClient *ollama.Client, stateStore *state.Store, ollamaConcurrency int) (*TelegramBot, error) {
	if ollamaConcurrency < 1 {
		ollamaConcurrency = 1
	}
	tb := &TelegramBot{
		whisper:           whisperClient,
		chatID:            chatID,
		filters:           f,
		store:             store,
		evolution:         evo,
		ollama:            ollamaClient,
		state:             stateStore,
		ollamaConcurrency: ollamaConcurrency,
	}

	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(tb.handleUpdate),
	}

	b, err := tgbot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	tb.bot = b
	return tb, nil
}

func (tb *TelegramBot) Start(ctx context.Context) {
	tb.bot.Start(ctx)
}

func (tb *TelegramBot) SendTranslation(ctx context.Context, filename, text string) error {
	msg := fmt.Sprintf("*%s*\n\n%s", tgbot.EscapeMarkdown(filename), tgbot.EscapeMarkdown(text))
	_, err := tb.bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    tb.chatID,
		Text:      msg,
		ParseMode: models.ParseModeMarkdown,
	})
	return err
}

func (tb *TelegramBot) handleUpdate(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	if update.Message.Chat.ID != tb.chatID {
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	if strings.HasPrefix(text, "/") {
		tb.handleCommand(ctx, b, update.Message, text)
		return
	}

	var fileID string
	var filename string

	switch {
	case update.Message.Voice != nil:
		fileID = update.Message.Voice.FileID
		filename = "voice.ogg"
	case update.Message.Audio != nil:
		fileID = update.Message.Audio.FileID
		filename = update.Message.Audio.FileName
	case update.Message.Document != nil:
		fileID = update.Message.Document.FileID
		filename = update.Message.Document.FileName
	default:
		tb.reply(ctx, b, update.Message.Chat.ID, "Send an audio file to translate, or /help for commands.")
		return
	}

	tb.reply(ctx, b, update.Message.Chat.ID, "Translating...")

	file, err := b.GetFile(ctx, &tgbot.GetFileParams{FileID: fileID})
	if err != nil {
		slog.Error("failed to get file from telegram", "error", err)
		tb.reply(ctx, b, update.Message.Chat.ID, "Failed to download file from Telegram.")
		return
	}

	fileURL := b.FileDownloadLink(file)
	resp, err := http.Get(fileURL)
	if err != nil {
		slog.Error("failed to download file", "url", fileURL, "error", err)
		return
	}
	defer resp.Body.Close()

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read file body", "error", err)
		return
	}

	translation, err := tb.whisper.Translate(filename, bytes.NewReader(audioData))
	if err != nil {
		slog.Error("whisper translation failed", "error", err)
		tb.reply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Translation failed: %v", err))
		return
	}

	tb.reply(ctx, b, update.Message.Chat.ID, translation)
}

func (tb *TelegramBot) handleCommand(ctx context.Context, b *tgbot.Bot, msg *models.Message, text string) {
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/help", "/start":
		tb.cmdHelp(ctx, b, msg)
	case "/status":
		tb.cmdStatus(ctx, b, msg)
	case "/groups":
		if len(args) > 0 {
			tb.cmdGroupsToggle(ctx, b, msg, args)
		} else {
			tb.cmdGroupsList(ctx, b, msg)
		}
	case "/group":
		tb.cmdGroupDetail(ctx, b, msg, args)
	case "/mute":
		tb.cmdMute(ctx, b, msg, args)
	case "/unmute":
		tb.cmdUnmute(ctx, b, msg, args)
	case "/muted":
		tb.cmdMuted(ctx, b, msg)
	case "/contacts":
		tb.cmdContacts(ctx, b, msg, args)
	case "/who":
		tb.cmdWho(ctx, b, msg, args)
	case "/history":
		tb.cmdHistory(ctx, b, msg, args)
	case "/recent":
		tb.cmdRecent(ctx, b, msg, args)
	case "/summary":
		tb.cmdSummary(ctx, b, msg)
	case "/translate":
		tb.cmdTranslate(ctx, b, msg, args)
	case "/replies":
		tb.cmdToggle(ctx, b, msg, args, "Reply drafts", "reply_drafts", tb.filters.SetReplyDrafts)
	case "/audio":
		tb.cmdToggle(ctx, b, msg, args, "Voice note translation", "translate_audio", tb.filters.SetTranslateAudio)
	case "/texts":
		tb.cmdToggle(ctx, b, msg, args, "Text translation", "translate_text", tb.filters.SetTranslateText)
	case "/model":
		tb.cmdModel(ctx, b, msg, args)
	case "/models":
		tb.cmdModels(ctx, b, msg)
	case "/category":
		tb.cmdCategory(ctx, b, msg, args)
	case "/categorise", "/categorize":
		tb.cmdCategorise(ctx, b, msg, args)
	case "/uncategorised", "/uncategorized":
		tb.cmdUncategorised(ctx, b, msg)
	case "/bootstrap":
		tb.cmdBootstrap(ctx, b, msg, args)
	case "/personal", "/family", "/business", "/service", "/commerce", "/government", "/spam":
		tb.cmdListCategory(ctx, b, msg, strings.TrimPrefix(cmd, "/"))
	case "/catstats":
		tb.cmdCatStats(ctx, b, msg)
	default:
		tb.reply(ctx, b, msg.Chat.ID, "Unknown command. /help")
	}
}

func (tb *TelegramBot) cmdHelp(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	tb.reply(ctx, b, msg.Chat.ID, ""+
		"Control:\n"+
		"  /status — dashboard\n"+
		"  /mute <name|jid> — mute contact\n"+
		"  /unmute <name|jid> — unmute\n"+
		"  /muted — list muted\n"+
		"  /groups on|off — toggle groups\n"+
		"  /replies on|off — toggle drafts\n"+
		"  /audio on|off — toggle voice\n"+
		"  /texts on|off — toggle text\n"+
		"  /model [name] — view/switch model\n"+
		"  /models — list available models\n"+
		"\n"+
		"Directory:\n"+
		"  /contacts [query] — search contacts\n"+
		"  /groups — list WhatsApp groups\n"+
		"  /group <name> — group details\n"+
		"  /who <jid> — resolve JID\n"+
		"\n"+
		"Categories:\n"+
		"  /category <name> — view contact category\n"+
		"  /categorise <name> <cat> — set manually\n"+
		"  /uncategorised — list unclassified\n"+
		"  /bootstrap [n] — LLM auto-classify\n"+
		"  /catstats — category breakdown\n"+
		"  /<cat> — list (personal/family/business/\n"+
		"    service/commerce/government/spam)\n"+
		"\n"+
		"History:\n"+
		"  /history <name> [n] — messages from contact\n"+
		"  /recent [n] — recent conversations\n"+
		"\n"+
		"Intelligence:\n"+
		"  /summary — today's message summary\n"+
		"  /translate <text> — EN → PT-BR")
}

func (tb *TelegramBot) cmdStatus(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	muted := tb.filters.ListMuted()
	groupsStatus := "allowed"
	if tb.filters.MuteGroups() {
		groupsStatus = "blocked"
	}
	onOff := func(v bool) string {
		if v {
			return "on"
		}
		return "off"
	}

	lines := []string{
		fmt.Sprintf("Model: %s", tb.filters.OllamaModel()),
		fmt.Sprintf("Groups: %s", groupsStatus),
		fmt.Sprintf("Audio: %s | Text: %s | Drafts: %s",
			onOff(tb.filters.TranslateAudio()),
			onOff(tb.filters.TranslateText()),
			onOff(tb.filters.ReplyDrafts())),
		fmt.Sprintf("Muted JIDs: %d", len(muted)),
	}

	if tb.store != nil {
		contactCount, msgCount, err := tb.store.ContactStats()
		if err == nil {
			lines = append(lines, fmt.Sprintf("Contacts: %d | Messages: %d", contactCount, msgCount))
		}
	}

	if tb.state != nil {
		stats, err := tb.state.CategoryStats()
		if err == nil && len(stats) > 0 {
			total := 0
			for _, c := range stats {
				total += c
			}
			lines = append(lines, fmt.Sprintf("Categorised: %d", total))
		}
	}

	tb.reply(ctx, b, msg.Chat.ID, strings.Join(lines, "\n"))
}

func (tb *TelegramBot) cmdGroupsToggle(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	switch strings.ToLower(args[0]) {
	case "on", "allow":
		tb.filters.SetMuteGroups(false)
		tb.persistBool("mute_groups", false)
		slog.Info("groups unmuted via telegram")
		tb.reply(ctx, b, msg.Chat.ID, "Group messages allowed.")
	case "off", "block":
		tb.filters.SetMuteGroups(true)
		tb.persistBool("mute_groups", true)
		slog.Info("groups muted via telegram")
		tb.reply(ctx, b, msg.Chat.ID, "Group messages blocked.")
	default:
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /groups on|off")
	}
}

func (tb *TelegramBot) cmdGroupsList(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	if tb.evolution == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Evolution API not configured.")
		return
	}

	groups, err := tb.evolution.FetchGroups(true)
	if err != nil {
		slog.Error("failed to fetch groups", "error", err)
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Failed: %v", err))
		return
	}

	if len(groups) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No groups found.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("WhatsApp Groups (%d):\n\n", len(groups)))
	for _, g := range groups {
		name := g.Subject
		if name == "" {
			name = "(unnamed)"
		}
		sb.WriteString(fmt.Sprintf("  %s — %d members\n", name, g.Size))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdGroupDetail(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.evolution == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Evolution API not configured.")
		return
	}
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /group <name>")
		return
	}

	query := strings.ToLower(strings.Join(args, " "))
	groups, err := tb.evolution.FetchGroups(true)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Failed: %v", err))
		return
	}

	var match *evolution.Group
	for i, g := range groups {
		if strings.Contains(strings.ToLower(g.Subject), query) {
			match = &groups[i]
			break
		}
	}
	if match == nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("No group matching %q", query))
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\nID: %s\nMembers: %d\n", match.Subject, match.ID, match.Size))
	for _, p := range match.Participants {
		role := ""
		if p.Admin != "" {
			role = " (" + p.Admin + ")"
		}
		name := p.ID
		if tb.store != nil {
			resolved := tb.store.ResolveName(p.ID)
			if resolved != p.ID {
				name = resolved
			}
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n", name, role))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdMute(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /mute <name or jid>")
		return
	}

	input := strings.Join(args, " ")
	jid, name, err := tb.resolveInput(input)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Could not resolve: %v", err))
		return
	}

	tb.filters.Mute(jid)
	if tb.state != nil {
		if err := tb.state.MuteJID(jid); err != nil {
			slog.Warn("failed to persist muted jid", "jid", jid, "error", err)
		}
	}
	slog.Info("jid muted via telegram", "jid", jid, "name", name)
	label := name
	if label == "" {
		label = jid
	}
	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Muted: %s (%s)", label, jid))
}

func (tb *TelegramBot) cmdUnmute(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /unmute <name or jid>")
		return
	}

	input := strings.Join(args, " ")
	jid, name, err := tb.resolveInput(input)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Could not resolve: %v", err))
		return
	}

	tb.filters.Unmute(jid)
	if tb.state != nil {
		if err := tb.state.UnmuteJID(jid); err != nil {
			slog.Warn("failed to persist unmuted jid", "jid", jid, "error", err)
		}
	}
	slog.Info("jid unmuted via telegram", "jid", jid, "name", name)
	label := name
	if label == "" {
		label = jid
	}
	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Unmuted: %s (%s)", label, jid))
}

func (tb *TelegramBot) cmdMuted(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	muted := tb.filters.ListMuted()
	if len(muted) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No JIDs muted.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Muted (%d):\n", len(muted)))
	for _, jid := range muted {
		name := jid
		if tb.store != nil {
			resolved := tb.store.ResolveName(jid)
			if resolved != jid {
				name = fmt.Sprintf("%s (%s)", resolved, jid)
			}
		}
		sb.WriteString(fmt.Sprintf("  %s\n", name))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdContacts(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.store == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Database not configured.")
		return
	}

	query := strings.Join(args, " ")
	if query == "" {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /contacts <search query>")
		return
	}

	results, err := tb.store.SearchContacts(query)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Search failed: %v", err))
		return
	}

	if len(results) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("No contacts matching %q", query))
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Contacts matching %q (%d):\n\n", query, len(results)))
	for _, c := range results {
		sb.WriteString(fmt.Sprintf("  %s\n    %s\n", c.PushName, c.RemoteJID))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdWho(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.store == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Database not configured.")
		return
	}
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /who <jid or name>")
		return
	}

	input := strings.Join(args, " ")
	jid, name, err := tb.resolveInput(input)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Not found: %v", err))
		return
	}

	if name != "" {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("%s\n%s", name, jid))
	} else {
		tb.reply(ctx, b, msg.Chat.ID, jid)
	}
}

func (tb *TelegramBot) cmdHistory(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.store == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Database not configured.")
		return
	}
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /history <name|jid> [count]")
		return
	}

	limit := 10
	nameArgs := args
	if len(args) > 1 {
		if n, err := strconv.Atoi(args[len(args)-1]); err == nil && n > 0 {
			limit = n
			nameArgs = args[:len(args)-1]
		}
	}
	if limit > 50 {
		limit = 50
	}

	input := strings.Join(nameArgs, " ")
	jid, name, err := tb.resolveInput(input)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Not found: %v", err))
		return
	}

	msgs, err := tb.store.MessageHistory(jid, limit)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Query failed: %v", err))
		return
	}

	if len(msgs) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No messages found.")
		return
	}

	label := name
	if label == "" {
		label = jid
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("History: %s (last %d)\n\n", label, len(msgs)))
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		dir := "<"
		if m.FromMe {
			dir = ">"
		}
		preview := m.TextPreview
		if preview == "" {
			preview = fmt.Sprintf("[%s]", m.MessageType)
		}
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		ts := m.Timestamp.Format("15:04")
		sb.WriteString(fmt.Sprintf("%s %s %s\n", ts, dir, preview))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdRecent(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.store == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Database not configured.")
		return
	}

	limit := 10
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 30 {
		limit = 30
	}

	msgs, err := tb.store.RecentConversations(limit)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Query failed: %v", err))
		return
	}

	if len(msgs) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No recent conversations.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Recent conversations (%d):\n\n", len(msgs)))
	for _, m := range msgs {
		name := m.PushName
		if name == "" {
			name = m.RemoteJID
		}
		preview := m.TextPreview
		if preview == "" {
			preview = fmt.Sprintf("[%s]", m.MessageType)
		}
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		ago := time.Since(m.Timestamp).Truncate(time.Minute)
		sb.WriteString(fmt.Sprintf("  %s — %s ago\n    %s\n", name, ago, preview))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdSummary(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	if tb.store == nil || tb.ollama == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Requires database and LLM.")
		return
	}

	tb.reply(ctx, b, msg.Chat.ID, "Generating summary...")

	msgs, err := tb.store.TodayMessages()
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Failed: %v", err))
		return
	}

	if len(msgs) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No messages in the last 24 hours.")
		return
	}

	var sb strings.Builder
	for _, m := range msgs {
		name := m.PushName
		if name == "" {
			name = m.RemoteJID
		}
		content := m.TextPreview
		if content == "" {
			content = fmt.Sprintf("[%s]", m.MessageType)
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", m.Timestamp.Format("15:04"), name, content))
	}

	tb.ollama.SetModel(tb.filters.OllamaModel())
	summary, err := tb.ollama.Summarise(sb.String())
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("LLM failed: %v", err))
		return
	}

	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Summary (%d messages):\n\n%s", len(msgs), summary))
}

func (tb *TelegramBot) cmdTranslate(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.ollama == nil {
		tb.reply(ctx, b, msg.Chat.ID, "LLM not configured.")
		return
	}
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /translate <english text>")
		return
	}

	text := strings.Join(args, " ")
	tb.ollama.SetModel(tb.filters.OllamaModel())
	result, err := tb.ollama.TranslateToPortuguese(text)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Translation failed: %v", err))
		return
	}

	tb.reply(ctx, b, msg.Chat.ID, result)
}

func (tb *TelegramBot) cmdToggle(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string, label, dbKey string, setter func(bool)) {
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Usage: /%s on|off", strings.ToLower(strings.Fields(label)[0])))
		return
	}
	v := strings.ToLower(args[0]) == "on"
	setter(v)
	tb.persistBool(dbKey, v)
	onOff := "off"
	if v {
		onOff = "on"
	}
	slog.Info("toggle via telegram", "feature", label, "enabled", v)
	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("%s %s.", label, onOff))
}

func (tb *TelegramBot) cmdModel(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Current model: %s\nUse /models to list available.", tb.filters.OllamaModel()))
		return
	}
	model := args[0]
	tb.filters.SetOllamaModel(model)
	if tb.ollama != nil {
		tb.ollama.SetModel(model)
	}
	tb.persistString("ollama_model", model)
	slog.Info("ollama model switched via telegram", "model", model)
	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Model switched to: %s", model))
}

func (tb *TelegramBot) cmdModels(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	if tb.ollama == nil {
		tb.reply(ctx, b, msg.Chat.ID, "LLM not configured.")
		return
	}

	available, err := tb.ollama.ListModels()
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Failed: %v", err))
		return
	}

	if len(available) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No models found in ollama.")
		return
	}

	current := tb.filters.OllamaModel()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available models (%d):\n\n", len(available)))
	for _, m := range available {
		sizeGB := float64(m.Size) / (1024 * 1024 * 1024)
		marker := "  "
		if m.Name == current {
			marker = "> "
		}
		sb.WriteString(fmt.Sprintf("%s%s (%.1f GB)\n", marker, m.Name, sizeGB))
	}
	sb.WriteString(fmt.Sprintf("\nUse /model <name> to switch."))
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdCategory(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.state == nil || tb.store == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Requires database.")
		return
	}
	if len(args) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /category <name|jid>")
		return
	}

	input := strings.Join(args, " ")
	jid, name, err := tb.resolveInput(input)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Not found: %v", err))
		return
	}

	cat, err := tb.state.GetCategory(jid)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		return
	}

	label := name
	if label == "" {
		label = jid
	}
	if cat == nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("%s: uncategorised", label))
		return
	}

	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("%s: %s\nReason: %s\nSource: %s\nUpdated: %s",
		label, cat.Category, cat.CategoryReason, cat.CategorySource,
		cat.UpdatedAt.Format("2006-01-02 15:04")))
}

func (tb *TelegramBot) cmdCategorise(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.state == nil || tb.store == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Requires database.")
		return
	}
	if len(args) < 2 {
		tb.reply(ctx, b, msg.Chat.ID, "Usage: /categorise <name|jid> <category>")
		return
	}

	category := strings.ToLower(args[len(args)-1])
	if !state.ValidCategories[category] {
		cats := make([]string, 0, len(state.ValidCategories))
		for c := range state.ValidCategories {
			cats = append(cats, c)
		}
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Invalid category. Valid: %s", strings.Join(cats, ", ")))
		return
	}

	nameArgs := args[:len(args)-1]
	input := strings.Join(nameArgs, " ")
	jid, name, err := tb.resolveInput(input)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Not found: %v", err))
		return
	}

	if err := tb.state.SetCategory(jid, category, "manual override", "manual"); err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		return
	}

	label := name
	if label == "" {
		label = jid
	}
	slog.Info("contact categorised manually", "jid", jid, "category", category)
	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("%s → %s", label, category))
}

func (tb *TelegramBot) cmdUncategorised(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	if tb.state == nil || tb.store == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Requires database.")
		return
	}

	jids, err := tb.state.ListUncategorised()
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		return
	}

	if len(jids) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "All contacts categorised!")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Uncategorised (%d):\n\n", len(jids)))
	for _, jid := range jids {
		name := tb.store.ResolveName(jid)
		if name == jid {
			sb.WriteString(fmt.Sprintf("  %s\n", jid))
		} else {
			sb.WriteString(fmt.Sprintf("  %s (%s)\n", name, jid))
		}
	}
	sb.WriteString("\nUse /bootstrap to auto-classify or /categorise <name> <cat> for manual.")
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdBootstrap(ctx context.Context, b *tgbot.Bot, msg *models.Message, args []string) {
	if tb.state == nil || tb.store == nil || tb.ollama == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Requires database and LLM.")
		return
	}

	count := 0
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			count = n
		}
	}

	countMsg := fmt.Sprintf("up to %d", count)
	if count == 0 {
		countMsg = "all"
	}
	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Classifying %s contacts...", countMsg))

	withHistory, err := tb.store.UncategorisedWithHistory(10, 3)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		return
	}

	if len(withHistory) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No contacts with message history to classify.")
		return
	}

	if count > 0 && len(withHistory) > count {
		withHistory = withHistory[:count]
	}

	tb.ollama.SetModel(tb.filters.OllamaModel())
	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Classifying %d contacts (concurrency %d)...", len(withHistory), tb.ollamaConcurrency))

	type classifyOutcome struct {
		ch     contacts.ContactWithHistory
		result ollama.ClassifyResult
		err    error
	}

	sem := make(chan struct{}, tb.ollamaConcurrency)
	outcomes := make([]classifyOutcome, len(withHistory))
	var wg sync.WaitGroup

	for i, ch := range withHistory {
		wg.Add(1)
		go func(idx int, contact contacts.ContactWithHistory) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result, err := tb.ollama.Classify(contact.PushName, contact.History)
			outcomes[idx] = classifyOutcome{ch: contact, result: result, err: err}
		}(i, ch)
	}
	wg.Wait()

	var classified int
	var sb strings.Builder
	for _, o := range outcomes {
		if o.err != nil {
			slog.Warn("classify failed", "jid", o.ch.JID, "error", o.err)
			continue
		}
		if err := tb.state.SetCategory(o.ch.JID, o.result.Category, o.result.Reason, "bootstrap"); err != nil {
			slog.Warn("save category failed", "jid", o.ch.JID, "error", err)
			continue
		}
		classified++
		sb.WriteString(fmt.Sprintf("  %s → %s\n", o.ch.PushName, o.result.Category))
	}

	if classified == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No contacts could be classified.")
		return
	}

	tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Classified %d/%d:\n\n%s", classified, len(withHistory), sb.String()))
}

func (tb *TelegramBot) cmdListCategory(ctx context.Context, b *tgbot.Bot, msg *models.Message, category string) {
	if tb.state == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Requires database.")
		return
	}

	contacts, err := tb.state.ListByCategory(category)
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		return
	}

	if len(contacts) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("No contacts in category %q.", category))
		return
	}

	var sb strings.Builder
	title := strings.ToUpper(category[:1]) + category[1:]
	sb.WriteString(fmt.Sprintf("%s (%d):\n\n", title, len(contacts)))
	for _, c := range contacts {
		name := c.JID
		if tb.store != nil {
			resolved := tb.store.ResolveName(c.JID)
			if resolved != c.JID {
				name = resolved
			}
		}
		sb.WriteString(fmt.Sprintf("  %s\n    %s\n", name, c.CategoryReason))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) cmdCatStats(ctx context.Context, b *tgbot.Bot, msg *models.Message) {
	if tb.state == nil {
		tb.reply(ctx, b, msg.Chat.ID, "Requires database.")
		return
	}

	stats, err := tb.state.CategoryStats()
	if err != nil {
		tb.reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		return
	}

	if len(stats) == 0 {
		tb.reply(ctx, b, msg.Chat.ID, "No contacts categorised yet. Try /bootstrap")
		return
	}

	var sb strings.Builder
	total := 0
	for _, count := range stats {
		total += count
	}
	sb.WriteString(fmt.Sprintf("Categories (%d total):\n\n", total))
	for cat, count := range stats {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", cat, count))
	}
	tb.reply(ctx, b, msg.Chat.ID, sb.String())
}

func (tb *TelegramBot) resolveInput(input string) (string, string, error) {
	if strings.Contains(input, "@") {
		name := ""
		if tb.store != nil {
			name = tb.store.ResolveName(input)
			if name == input {
				name = ""
			}
		}
		return input, name, nil
	}

	if tb.store == nil {
		return "", "", fmt.Errorf("database not configured, use full JID")
	}

	return tb.store.ResolveJID(input)
}

func (tb *TelegramBot) persistBool(key string, value bool) {
	if tb.state == nil {
		return
	}
	if err := tb.state.SetBool(key, value); err != nil {
		slog.Warn("failed to persist setting", "key", key, "error", err)
	}
}

func (tb *TelegramBot) persistString(key, value string) {
	if tb.state == nil {
		return
	}
	if err := tb.state.SetString(key, value); err != nil {
		slog.Warn("failed to persist setting", "key", key, "error", err)
	}
}

func (tb *TelegramBot) reply(ctx context.Context, b *tgbot.Bot, chatID int64, text string) {
	if len(text) > 4000 {
		text = text[:4000] + "\n... (truncated)"
	}
	b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
}
