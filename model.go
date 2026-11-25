package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type SteamSource struct {
	AppID int    `json:"app_id"`
	Name  string `json:"name"`
}
type WorkshopSource struct {
	FileID     string `json:"file_id"`
	Name       string `json:"name"`
	ParentGame string `json:"parent_game,omitempty"`
}
type Config struct {
	Steam                 []SteamSource    `json:"steam"`
	Workshop              []WorkshopSource `json:"workshop"`
	GitHub                []string         `json:"github"`
	RSS                   []RSSSource      `json:"rss"`
	Interval              int              `json:"interval"`
	AutoMonitor           bool             `json:"auto_monitor"`
	AutoApproved          bool             `json:"auto_approved"`
	GroupID               string           `json:"group_id"`
	SkipHotfixes          bool             `json:"skip_hotfixes"`
	Discovery             bool             `json:"discovery"`
	AIProvider            string           `json:"ai_provider"`
	AIModel               string           `json:"ai_model"`
	LocalAIEnabled        bool             `json:"local_ai_enabled"`
	AutoRewrite           bool             `json:"auto_rewrite"`
	AutoImages            bool             `json:"auto_images"`
	UpdateRepo            string           `json:"update_repo,omitempty"`
	ReleaseLeadDays       int              `json:"release_lead_days"`
	ReleaseTop            int              `json:"release_top"`
	ReleasePreviews       bool             `json:"release_previews"`
	ReleaseDayPosts       bool             `json:"release_day_posts"`
	ReleaseIndie          bool             `json:"release_indie"`
	ReleaseWatch          []SteamSource    `json:"release_watch,omitempty"`
	HybridApproved        []int            `json:"hybrid_approved,omitempty"`
	CoreGames             []string         `json:"core_games,omitempty"`
	IndieGames            []string         `json:"indie_games,omitempty"`
	PostSlots             string           `json:"post_slots"`
	UrgentThreshold       int              `json:"urgent_threshold"`
	VKPostponedOnApproval bool             `json:"vk_postponed_on_approval"`
	VKBrowserMode         bool             `json:"vk_browser_mode"`
	VKCommunityURL        string           `json:"vk_community_url"`
}
type RSSSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type Item struct {
	ID                   string       `json:"id"`
	Source               string       `json:"source"`
	Game                 string       `json:"game"`
	Title                string       `json:"title"`
	URL                  string       `json:"url"`
	Time                 time.Time    `json:"time"`
	Excerpt              string       `json:"excerpt"`
	Category             string       `json:"category"`
	ParentGame           string       `json:"parent_game,omitempty"`
	EditorialOverride    string       `json:"editorial_override,omitempty"`
	EditorialBucket      string       `json:"editorial_bucket,omitempty"`
	EventKind            string       `json:"event_kind,omitempty"`
	State                string       `json:"state"`
	Draft                string       `json:"draft"`
	Images               []string     `json:"images"`
	Verified             bool         `json:"verified"`
	VKPostID             int64        `json:"vk_post_id,omitempty"`
	Error                string       `json:"error,omitempty"`
	Comment              string       `json:"comment"`
	CommentID            int64        `json:"comment_id,omitempty"`
	CommentState         string       `json:"comment_state,omitempty"`
	RewriteStatus        string       `json:"rewrite_status,omitempty"`
	AIOptions            *AIVariants  `json:"ai_options,omitempty"`
	MediaSources         []string     `json:"media_sources,omitempty"`
	MediaLabels          []string     `json:"media_labels,omitempty"`
	MediaFocus           string       `json:"media_focus,omitempty"`
	MediaStatus          string       `json:"media_status,omitempty"`
	MediaError           string       `json:"media_error,omitempty"`
	Priority             int          `json:"priority"`
	ScheduledAt          time.Time    `json:"scheduled_at,omitempty"`
	PublishedAt          time.Time    `json:"published_at,omitempty"`
	DiffSummary          string       `json:"diff_summary,omitempty"`
	VKStats              *VKMetrics   `json:"vk_stats,omitempty"`
	Awards               *CoverAwards `json:"awards,omitempty"`
	AwardsOriginal       string       `json:"awards_original,omitempty"`
	AwardsHero           string       `json:"awards_hero,omitempty"`
	VKQueueError         string       `json:"vk_queue_error,omitempty"`
	VKBrowserPreparedAt  time.Time    `json:"vk_browser_prepared_at,omitempty"`
	VKBrowserMarkedAt    time.Time    `json:"vk_browser_marked_at,omitempty"`
	VKBrowserExportDir   string       `json:"vk_browser_export_dir,omitempty"`
	VKBrowserFingerprint string       `json:"vk_browser_fingerprint,omitempty"`
	VKBrowserAutoFillAt  time.Time    `json:"vk_browser_auto_fill_at,omitempty"`
	VKBrowserAutoPhotos  bool         `json:"vk_browser_auto_photos,omitempty"`
}
type AuditEvent struct {
	At      time.Time `json:"at"`
	Action  string    `json:"action"`
	ItemID  string    `json:"item_id"`
	Details string    `json:"details"`
}
type saved struct {
	Audit        []AuditEvent   `json:"audit"`
	Version      int            `json:"version"`
	Discovered   []SteamSource  `json:"discovered"`
	ScanCursor   int            `json:"scan_cursor"`
	Config       Config         `json:"config"`
	Items        []*Item        `json:"items"`
	Radar        []ReleaseGame  `json:"radar,omitempty"`
	RadarChecked time.Time      `json:"radar_checked,omitempty"`
	RadarError   string         `json:"radar_error,omitempty"`
	AwardCatalog []CatalogAward `json:"award_catalog,omitempty"`
	LastCheck    time.Time      `json:"last_check"`
	LastError    string         `json:"last_error"`
}
type app struct {
	mu         sync.Mutex
	dataDir    string
	state      saved
	checking   bool
	posting    bool
	token      string // Live VK token. Encrypted vault persistence is managed separately.
	vkVault    string // Windows DPAPI credential file in user profile; never in state.json.
	vkVaultErr string
	aiKey      string // RAM only, never persisted.
	aiBusy     bool
	radarBusy  bool
	secret     string
}

func defaultConfig() Config {
	return Config{
		Steam: []SteamSource{
			{281990, "Stellaris"}, {394360, "Hearts of Iron IV"}, {1151640, "Crusader Kings III"},
			{529340, "Victoria 3"}, {1142710, "Total War: Warhammer III"},
			{1669000, "Age of Wonders 4"}, {294100, "RimWorld"}, {1466860, "Age of Empires IV"},
		},
		Workshop: []WorkshopSource{}, GitHub: []string{}, RSS: []RSSSource{}, GroupID: "211159240",
		CoreGames: defaultCoreGames(), IndieGames: []string{}, ReleaseLeadDays: 5, ReleaseTop: 15, ReleasePreviews: true, ReleaseDayPosts: true, ReleaseIndie: true, ReleaseWatch: []SteamSource{}, Interval: 30, AutoMonitor: true, AutoApproved: false, SkipHotfixes: false, Discovery: true, AIProvider: "none", AIModel: "qwen3:14b", AutoRewrite: false, AutoImages: true, VKPostponedOnApproval: false, VKBrowserMode: true, VKCommunityURL: "https://vk.ru/malowargaming", PostSlots: "10:00,14:00,18:00,21:00", UrgentThreshold: 90,
	}
}
func newApp(dir string) (*app, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	a := &app{dataDir: dir, secret: hex.EncodeToString(b), state: saved{Version: 10, Config: defaultConfig(), Items: []*Item{}, AwardCatalog: defaultAwardCatalog()}}
	buf, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		var s saved
		if json.Unmarshal(buf, &s) == nil {
			a.state = s
		}
	}
	if a.state.Version < 2 {
		a.state.Version = 2
		a.state.Config.Discovery = true
		a.state.Config.AIProvider = "none"
		a.state.Config.AIModel = "qwen2.5:7b"
		a.state.Config.AutoRewrite = false
	}
	if a.state.Version < 3 {
		a.state.Config.AutoImages = true
		a.state.Version = 3
	}
	if a.state.Version < 4 {
		a.state.Version = 4
		a.state.Config.PostSlots = "10:00,14:00,18:00,21:00"
		a.state.Config.UrgentThreshold = 90
		// Earlier auto-send did not have schedule safety. Require explicit scheduling.
		a.state.Config.AutoApproved = false
	}
	if a.state.Version < 5 {
		// Upgrading from 1.3 never enables a GPU-heavy model without explicit consent.
		a.state.Config.LocalAIEnabled = false
		if a.state.Config.AIProvider == "ollama" && a.state.Config.AIModel == "qwen2.5:7b" {
			a.state.Config.AIModel = "qwen3:14b"
		}
		if a.state.Config.AIProvider == "openai" && strings.HasPrefix(a.state.Config.AIModel, "qwen") {
			a.state.Config.AIModel = "gpt-4.1-mini"
		}
		a.state.Version = 5
	}
	if a.state.Version < 6 {
		if len(a.state.Config.CoreGames) == 0 {
			a.state.Config.CoreGames = defaultCoreGames()
		}
		a.state.Version = 6
	}
	if a.state.Version < 7 {
		// A migration preserves every existing item, editor decision and configured source.
		// The approved release radar defaults apply only on first upgrade.
		a.state.Config.ReleaseLeadDays = 5
		a.state.Config.ReleaseTop = 15
		a.state.Config.ReleasePreviews = true
		a.state.Config.ReleaseDayPosts = true
		a.state.Config.ReleaseIndie = true
		a.state.Version = 7
	}
	if a.state.Version < 8 {
		// Never drop existing sources, drafts, approvals, or prior ranking snapshots.
		a.state.Version = 8
	}
	if a.state.Version < 9 {
		// Pre-existing approved drafts are NOT automatically submitted to VK.
		a.state.Config.VKPostponedOnApproval = true
		a.state.Version = 9
	}
	if a.state.Version < 10 {
		// Browser publisher is opt-in per approved item; no legacy queue is sent.
		a.state.Config.VKBrowserMode = true
		a.state.Config.VKPostponedOnApproval = false
		a.state.Version = 10
	}
	if a.state.Config.VKCommunityURL == "" {
		a.state.Config.VKCommunityURL = "https://vk.ru/malowargaming"
	}
	if a.state.Config.GroupID == "" {
		// Dedicated community default; never overwrite another configured group.
		a.state.Config.GroupID = "211159240"
	}
	if len(a.state.AwardCatalog) == 0 {
		a.state.AwardCatalog = defaultAwardCatalog()
	}
	for _, it := range a.state.Items {
		if it.State == "vk_sending" {
			it.State = "uncertain"
			it.Error = "Проверь отложенные VK: отправка могла завершиться до аварийного закрытия"
		}
	}
	if a.state.Config.AIProvider != "ollama" {
		a.state.Config.LocalAIEnabled = false
	}
	if a.state.Config.PostSlots == "" {
		a.state.Config.PostSlots = "10:00,14:00,18:00,21:00"
	}
	if a.state.Config.UrgentThreshold < 50 || a.state.Config.UrgentThreshold > 100 {
		a.state.Config.UrgentThreshold = 90
	}
	for _, it := range a.state.Items {
		if it.State == "publishing" {
			it.State = "uncertain"
			it.Error = "После аварийного закрытия проверь стену VK: публикация могла пройти"
		}
		if it.Priority == 0 {
			it.Priority = scoreNews(*it)
		}
		if it.MediaStatus == "working" {
			it.MediaStatus = "pending"
		}
		if it.Comment == "" {
			it.Comment = defaultComment(*it)
		}
		if it.State == "published" && it.VKPostID > 0 && it.CommentID == 0 && it.CommentState == "" {
			it.CommentState = "check"
		}
	}
	if a.state.Items == nil {
		a.state.Items = []*Item{}
	}
	if a.state.Config.Interval < 5 || a.state.Config.Interval > 1440 {
		a.state.Config.Interval = 30
	}
	return a, nil
}
func (a *app) persistLocked() error {
	b, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(a.dataDir, "state.json")
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	// Rename on Windows can require removing the destination. Preserve a fallback backup.
	if _, err = os.Stat(path); err == nil {
		_ = os.Remove(path + ".bak")
		if err = os.Rename(path, path+".bak"); err != nil {
			return err
		}
	}
	if err = os.Rename(tmp, path); err != nil {
		if _, oldErr := os.Stat(path + ".bak"); oldErr == nil {
			_ = os.Rename(path+".bak", path)
		}
		return err
	}
	return nil
}
func (a *app) getItemLocked(id string) *Item {
	for _, it := range a.state.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}
func (a *app) snapshot() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := make([]*Item, 0, len(a.state.Items))
	for _, it := range a.state.Items {
		copyIt := *it
		copyIt.Images = append([]string(nil), it.Images...)
		copyIt.MediaSources = append([]string(nil), it.MediaSources...)
		copyIt.MediaLabels = append([]string(nil), it.MediaLabels...)
		copyIt.EditorialBucket = editorialBucket(copyIt, a.state.Config)
		copyIt.EventKind = eventKind(copyIt)
		if it.AIOptions != nil {
			v := *it.AIOptions
			v.ShortWarnings = append([]string(nil), v.ShortWarnings...)
			v.ExpandedWarnings = append([]string(nil), v.ExpandedWarnings...)
			copyIt.AIOptions = &v
		}
		items = append(items, &copyIt)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
	return map[string]any{"audit": a.state.Audit, "config": a.state.Config, "items": items, "last_check": a.state.LastCheck, "last_error": a.state.LastError, "checking": a.checking, "posting": a.posting, "vk_connected": a.token != "", "vk_token_saved": a.vkVault != "" && a.token != "", "vk_token_storage_error": a.vkVaultErr, "data_dir": a.dataDir, "discovered_count": len(a.state.Discovered), "ai_key_connected": a.aiKey != "", "ai_busy": a.aiBusy, "local_ai_enabled": a.state.Config.LocalAIEnabled, "radar": a.radarTopLocked(), "radar_all": a.radarAllLocked(), "radar_checked": a.state.RadarChecked, "radar_error": a.state.RadarError, "radar_busy": a.radarBusy, "award_catalog": a.state.AwardCatalog}
}
func (a *app) updateConfig(c Config) error {
	if _, err := validateSlots(c.PostSlots); err != nil {
		return err
	}
	if c.UrgentThreshold < 50 || c.UrgentThreshold > 100 {
		return errors.New("Порог срочности: 50–100")
	}
	if c.Interval < 5 || c.Interval > 1440 {
		return errors.New("Интервал: от 5 до 1440 минут")
	}
	if !editorialConfigValid(c.CoreGames) || !editorialConfigValid(c.IndieGames) {
		return errors.New("Список основных и инди-игр: не более 100 названий, до 90 знаков каждое")
	}
	if c.ReleaseLeadDays < 1 || c.ReleaseLeadDays > 30 || c.ReleaseTop < 1 || c.ReleaseTop > 30 {
		return errors.New("Релизный радар: подготовка за 1–30 дней, топ от 1 до 30")
	}
	if len(c.HybridApproved) > 250 {
		return errors.New("Слишком много подтверждённых гибридных стратегий")
	}
	for _, id := range c.HybridApproved {
		if id <= 0 {
			return errors.New("Некорректный AppID гибридной стратегии")
		}
	}
	if len(c.ReleaseWatch) > 100 {
		return errors.New("Слишком много игр для релизного радара (не более 100)")
	}
	for _, game := range c.ReleaseWatch {
		if game.AppID <= 0 || strings.TrimSpace(game.Name) == "" || len([]rune(game.Name)) > 90 {
			return errors.New("Неверный Steam AppID или название для релизного радара")
		}
	}
	if len(c.Steam) > 60 || len(c.Workshop) > 100 || len(c.GitHub) > 60 || len(c.RSS) > 60 {
		return errors.New("Слишком много источников")
	}
	for _, s := range c.Steam {
		if s.AppID <= 0 || strings.TrimSpace(s.Name) == "" {
			return errors.New("Неверные данные Steam")
		}
	}
	for _, s := range c.Workshop {
		if strings.Trim(s.FileID, "0123456789") != "" || s.FileID == "" || len([]rune(s.ParentGame)) > 90 {
			return errors.New("Неверный Workshop ID")
		}
	}
	for _, r := range c.GitHub {
		p := strings.Split(r, "/")
		if len(p) != 2 || strings.TrimSpace(p[0]) == "" || strings.TrimSpace(p[1]) == "" {
			return fmt.Errorf("Неверный GitHub repo: %s", r)
		}
	}
	for _, r := range c.RSS {
		if !strings.HasPrefix(r.URL, "https://") || r.Name == "" {
			return fmt.Errorf("Неверный RSS: %s", r.Name)
		}
	}
	if c.AIProvider != "none" && c.AIProvider != "ollama" && c.AIProvider != "openai" {
		return errors.New("ИИ-провайдер: none, ollama или openai")
	}
	if c.AIProvider != "ollama" {
		c.LocalAIEnabled = false
	}
	if len(c.AIModel) > 100 {
		return errors.New("Слишком длинное имя модели")
	}
	if c.UpdateRepo != "" && !validUpdateRepo(c.UpdateRepo) {
		return errors.New("Неверный репозиторий обновлений: укажи owner/repo")
	}
	if c.GroupID != "" && strings.Trim(c.GroupID, "0123456789") != "" {
		return errors.New("ID группы VK должен быть числом без минуса")
	}
	a.mu.Lock()
	if a.aiBusy && a.state.Config.LocalAIEnabled && !c.LocalAIEnabled {
		a.mu.Unlock()
		return errors.New("ИИ сейчас обрабатывает новость. Дождись завершения перед выключением")
	}
	previous := a.state.Config
	// Older settings clients do not send the new field. Never discard editor decisions.
	if c.HybridApproved == nil {
		c.HybridApproved = append([]int(nil), previous.HybridApproved...)
	}
	url, err := normalizeVKCommunity(c.VKCommunityURL)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	c.VKCommunityURL = url
	if previous.VKCommunityURL != c.VKCommunityURL {
		for _, item := range a.state.Items {
			if item.State != "vk_browser_marked" {
				item.VKBrowserPreparedAt = time.Time{}
				item.VKBrowserExportDir = ""
				item.VKBrowserFingerprint = ""
			}
		}
	}
	c.AutoApproved = false // Publication is opt-in per news approval.
	// The browser workflow never silently re-enables VK API posting.
	c.VKBrowserMode = true
	c.VKPostponedOnApproval = false
	a.state.Config = c
	err = a.persistLocked()
	a.mu.Unlock()
	if err == nil && previous.AIProvider == "ollama" && previous.LocalAIEnabled &&
		(!c.LocalAIEnabled || c.AIProvider != "ollama" || c.AIModel != previous.AIModel) {
		go func(model string) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = unloadOllamaModel(ctx, model)
		}(previous.AIModel)
	}
	return err
}
func (a *app) updateItem(id, draft, comment, state string, verified bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil {
		return errors.New("Новость не найдена")
	}
	if it.State == "published" || it.State == "publishing" || it.State == "uncertain" || it.State == "scheduled" || it.State == "vk_pending" || it.State == "vk_sending" || it.State == "vk_postponed" || it.State == "vk_browser_marked" {
		return errors.New("Опубликованную или отправляемую запись изменять нельзя")
	}
	if len([]rune(draft)) > 15000 || len([]rune(comment)) > 9000 {
		return errors.New("Текст слишком длинный")
	}
	if state != "" && state != "new" && state != "approved" && state != "rejected" {
		return errors.New("Недопустимый статус")
	}
	if state == "approved" {
		if len(it.Images) < 3 || len(it.Images) > 7 {
			return errors.New("Для одобрения нужно от 3 до 7 изображений")
		}
		if !verified {
			return errors.New("Перед одобрением подтверди проверку первоисточника")
		}
		if len([]rune(strings.TrimSpace(draft))) < 50 {
			return errors.New("Слишком короткий текст для публикации")
		}
		if !hasGameHashtags(draft, it.Game) {
			return errors.New("Для публикации нужны 4–5 уникальных хэштегов отдельной строкой, включая слитное название игры")
		}
		if strings.Contains(draft, "Перед публикацией проверьте даты") {
			return errors.New("Сначала отредактируй текст и убери служебное напоминание о проверке")
		}
	}
	// New browser mode includes only the reviewed body. No first-comment workflow.
	it.Draft = strings.TrimSpace(draft)
	it.Comment = ""
	it.VKBrowserPreparedAt = time.Time{}
	it.VKBrowserExportDir = ""
	it.VKBrowserFingerprint = ""
	it.VKBrowserAutoFillAt = time.Time{}
	it.VKBrowserAutoPhotos = false
	it.Verified = verified
	if state == "approved" && !a.state.Config.VKBrowserMode && a.state.Config.VKPostponedOnApproval {
		if !validGroup(a.state.Config.GroupID) {
			return errors.New("Для автоматической отложки сначала укажи числовой ID сообщества VK в настройках")
		}
		slot, err := a.nextSlotLocked(time.Now().UTC().Add(10*time.Minute), it.ID)
		if err != nil {
			return err
		}
		it.ScheduledAt = slot
		it.State = "vk_pending"
		it.VKQueueError = ""
		a.auditLocked("vk_enqueue", it.ID, "Одобрено; ожидает создания отложенного поста VK: "+slot.In(moscow).Format("02.01.2006 15:04"))
		return a.persistLocked()
	}
	if state != "" {
		it.State = state
		a.auditLocked("editor", it.ID, "Статус: "+state)
	}
	return a.persistLocked()
}
func uid(parts ...string) string { // Stable opaque identifier, safe in URLs and file paths.
	return strings.Join(parts, ":")
}
func draftText(i Item) string {
	summary := strings.TrimSpace(i.Excerpt)
	if len([]rune(summary)) > 440 {
		summary = string([]rune(summary)[:440]) + "..."
	}
	header := editorialHeadline(i.Game, i.Title)
	return ensureGameHashtags(header+"\n\n"+"Перед публикацией проверьте даты, условия и детали по первоисточнику.\n\n"+summary, i.Category, i.Game)
}
func classify(title, source string) string {
	s := strings.ToLower(title)
	switch {
	case source == "Workshop":
		return "Моды"
	case strings.Contains(s, "release") || strings.Contains(s, "launch") || strings.Contains(s, "вышл") || strings.Contains(s, "релиз"):
		return "Релизы"
	case strings.Contains(s, "dlc") || strings.Contains(s, "expansion") || strings.Contains(s, "дополнен"):
		return "DLC"
	case strings.Contains(s, "patch") || strings.Contains(s, "update") || strings.Contains(s, "hotfix") || strings.Contains(s, "обновлен"):
		return "Обновления"
	default:
		return "Новости"
	}
}
