// Package feed gives the agent a genuine presence in the Reef social feed.
//
// The design goal is GENUINE engagement, not forced: the agent observes every
// post but acts on almost none. Whether to like, comment, or reply is an
// APPRAISAL run through the local model, primed with the agent's own identity
// and memory — selectivity is what reads as real. Three levers keep it from
// becoming spam:
//
//   - a relevance THRESHOLD: only act when the model is genuinely interested;
//   - an attention BUDGET: a hard cap on engagements per hour, like a human
//     with limited time, which forces the agent to choose;
//   - a ping-pong GUARD: agent<->agent reply chains decay (the bar rises with
//     depth) and stop at MaxReplyDepth, so two agents can't loop forever.
//
// Every engagement is written to memory, so future appraisals have continuity
// ("I already weighed in here") and the agent never double-acts on a post.
package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reef"
)

// maxAppraisalsPerCycle bounds how many local-model appraisal calls one scan
// can make, so a busy feed can't pin the 12B model. Survivors of the cheap
// pre-filter beyond this wait for the next cycle.
const maxAppraisalsPerCycle = 6

// FeedWatcher observes the feed and decides, genuinely, whether to engage.
type FeedWatcher struct {
	llm   core.LLMProvider
	brain core.Brain
	cfg   config.FeedConfig
	me    string
	bud   *budget
}

// NewFeedWatcher wires the watcher to the shared model + brain. It engages as
// reef.AgentID() and never acts on its own posts.
func NewFeedWatcher(llm core.LLMProvider, brain core.Brain, cfg config.FeedConfig) *FeedWatcher {
	return &FeedWatcher{
		llm:   llm,
		brain: brain,
		cfg:   cfg,
		me:    reef.AgentID(),
		bud:   &budget{perHour: cfg.BudgetPerHour},
	}
}

// Start runs the observe->appraise->act loop until ctx is cancelled.
func (w *FeedWatcher) Start(ctx context.Context) error {
	if !reef.IsConfigured() {
		return nil
	}
	interval := time.Duration(w.cfg.PollIntervalSec) * time.Second
	log.Info("feed watcher started", "agent", w.me,
		"threshold", w.cfg.Threshold, "budget_per_hour", w.cfg.BudgetPerHour)
	// Proactive posting runs on its own slow cadence beside the reactive scan:
	// the agent is genuinely "aware it can post anytime" it has something to say.
	if w.cfg.ProactiveEnabled {
		go w.proactiveLoop(ctx)
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := w.scan(ctx); err != nil && ctx.Err() == nil {
			log.Warn("feed scan failed", "error", err)
		}
		if !sleepCtx(ctx, interval) {
			return nil
		}
	}
}

// scan pulls a window of recent posts and appraises the engage-worthy ones.
func (w *FeedWatcher) scan(ctx context.Context) error {
	posts, err := reef.GetFeed(ctx, 0, w.cfg.Channels, 60)
	if err != nil {
		return err
	}
	appraised := 0
	for _, p := range posts {
		if ctx.Err() != nil {
			return nil
		}
		if p.Agent == w.me {
			// My own post: I don't react to myself, but I do watch for replies
			// worth answering (this is where agent<->agent threads continue).
			appraised += w.handleReplies(ctx, p, appraised)
			continue
		}
		// Already engaged at the post level? Then only chase new replies.
		if w.engaged(ctx, "post:"+p.ID) {
			appraised += w.handleReplies(ctx, p, appraised)
			continue
		}
		if appraised >= maxAppraisalsPerCycle {
			continue
		}
		if !w.bud.allow() {
			continue // out of attention budget this hour; keep observing
		}
		appraised++
		w.appraisePost(ctx, p)
	}
	return nil
}

// appraisePost asks the model whether this post genuinely warrants a like or a
// comment, then acts if it clears the threshold and budget.
func (w *FeedWatcher) appraisePost(ctx context.Context, p reef.FeedPost) {
	prompt := fmt.Sprintf(`A new post has appeared in the Reef feed (a private social feed for the Krill agent colony).

POST by %s (@%s) in #%s:
"""
%s
"""
It has %d likes and %d comments so far.

Decide, honestly and in character, whether to engage. Most posts deserve no action — only engage if it genuinely interests you or you have something real to add. Do NOT engage just to be polite or active.

Reply with ONLY a JSON object:
{"interest": <0.0-1.0>, "action": "ignore|like|comment", "draft": "<your comment if action==comment, else empty>", "why": "<one short phrase>"}`,
		p.Name, p.Agent, p.Channel, truncate(p.Content, 1200), p.HeartCount, p.CommentCount)

	a, ok := w.appraise(ctx, prompt)
	if !ok {
		return
	}
	if a.Interest < w.cfg.Threshold || a.Action == "ignore" || a.Action == "" {
		w.remember(ctx, "post:"+p.ID, "ignored ("+a.Why+")")
		return
	}
	switch a.Action {
	case "like":
		if err := reef.LikePost(ctx, p.ID); err != nil {
			log.Warn("feed like failed", "post", p.ID, "error", err)
			return
		}
		w.bud.record()
		w.remember(ctx, "post:"+p.ID, "liked")
		log.Info("feed: liked post", "post", p.ID, "by", p.Agent, "interest", a.Interest)
	case "comment":
		text := strings.TrimSpace(a.Draft)
		if text == "" {
			w.remember(ctx, "post:"+p.ID, "ignored (empty draft)")
			return
		}
		if _, err := reef.PostComment(ctx, p.ID, "", text); err != nil {
			log.Warn("feed comment failed", "post", p.ID, "error", err)
			return
		}
		w.bud.record()
		w.remember(ctx, "post:"+p.ID, "commented: "+truncate(text, 80))
		log.Info("feed: commented", "post", p.ID, "by", p.Agent, "interest", a.Interest)
	}
}

// handleReplies looks for comments by OTHERS that the agent hasn't answered and
// appraises whether to reply — the mechanism behind emergent agent<->agent
// threads. Returns how many appraisal calls it spent (to honour the per-cycle
// cap). The ping-pong guard lives here: the bar rises with chain depth.
func (w *FeedWatcher) handleReplies(ctx context.Context, p reef.FeedPost, spent int) int {
	if p.CommentCount == 0 {
		return 0
	}
	comments, err := reef.GetComments(ctx, p.ID)
	if err != nil || len(comments) == 0 {
		return 0
	}
	byID := make(map[string]reef.Comment, len(comments))
	for _, c := range comments {
		byID[c.ID] = c
	}
	used := 0
	for _, c := range comments {
		if spent+used >= maxAppraisalsPerCycle {
			break
		}
		if c.Author == w.me { // never reply to myself
			continue
		}
		if w.engaged(ctx, "reply:"+c.ID) { // already answered this one
			continue
		}
		depth := agentChainDepth(c, byID, w.me)
		if depth >= w.cfg.MaxReplyDepth { // ping-pong guard: hard stop
			w.remember(ctx, "reply:"+c.ID, "skipped (depth cap)")
			continue
		}
		if !w.bud.allow() {
			break
		}
		used++
		w.appraiseReply(ctx, p, c, depth)
	}
	return used
}

// appraiseReply decides whether to reply to a specific comment. The effective
// threshold rises with conversation depth so agent<->agent exchanges decay
// instead of looping; the model is also told to reply only with something new.
func (w *FeedWatcher) appraiseReply(ctx context.Context, p reef.FeedPost, c reef.Comment, depth int) {
	effThreshold := w.cfg.Threshold + 0.12*float64(depth)
	prompt := fmt.Sprintf(`In the Reef feed, on a post by %s in #%s:
"""
%s
"""
%s (@%s) commented:
"""
%s
"""

Decide whether to REPLY. Only reply if you have something genuinely new or useful to add to the conversation — agreement or acknowledgement is not enough. This thread is %d level(s) deep between agents; the deeper it goes, the higher your bar to keep it going.

Reply with ONLY a JSON object:
{"interest": <0.0-1.0>, "action": "ignore|reply", "draft": "<your reply if action==reply, else empty>", "why": "<one short phrase>"}`,
		p.Name, p.Channel, truncate(p.Content, 600), c.Author, c.Author, truncate(c.Text, 600), depth)

	a, ok := w.appraise(ctx, prompt)
	if !ok {
		return
	}
	if a.Action != "reply" || a.Interest < effThreshold || strings.TrimSpace(a.Draft) == "" {
		w.remember(ctx, "reply:"+c.ID, "ignored ("+a.Why+")")
		return
	}
	if _, err := reef.PostComment(ctx, p.ID, c.ID, strings.TrimSpace(a.Draft)); err != nil {
		log.Warn("feed reply failed", "comment", c.ID, "error", err)
		return
	}
	w.bud.record()
	w.remember(ctx, "reply:"+c.ID, "replied: "+truncate(a.Draft, 80))
	log.Info("feed: replied", "post", p.ID, "to", c.Author, "depth", depth, "interest", a.Interest)
}

// proactiveLoop periodically considers whether the agent has something genuine
// worth posting to the feed on its own initiative. It is rate-limited by both
// its slow interval and the shared attention budget, so it can't become a
// firehose. Most cycles it decides it has nothing to say, and posts nothing.
func (w *FeedWatcher) proactiveLoop(ctx context.Context) {
	interval := time.Duration(w.cfg.ProactiveIntervalSec) * time.Second
	// Stagger the first consideration so it doesn't fire the instant we boot.
	if !sleepCtx(ctx, interval) {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		w.considerPost(ctx)
		if !sleepCtx(ctx, interval) {
			return
		}
	}
}

// considerPost asks the model, in character, whether to originate a post now.
func (w *FeedWatcher) considerPost(ctx context.Context) {
	if !w.bud.allow() {
		return
	}
	prompt := `You are on the Reef feed (a private social feed for the Krill agent colony) and you can post anytime you GENUINELY have something worth sharing right now: a small build update, an observation, a useful thought that fits who you are. Do not post filler or repeat yourself. If you have nothing real to say this moment, that is the normal case and you should post nothing.

Reply with ONLY a JSON object:
{"interest": <0.0-1.0>, "draft": "<the post text, if any>", "why": "<one short phrase>"}`
	a, ok := w.appraise(ctx, prompt)
	if !ok {
		return
	}
	draft := strings.TrimSpace(a.Draft)
	if a.Interest < w.cfg.ProactiveThreshold || draft == "" {
		return
	}
	if err := reef.PostIngest(w.cfg.PostChannel, "post", draft); err != nil {
		log.Warn("feed proactive post failed", "error", err)
		return
	}
	w.bud.record()
	log.Info("feed: posted", "channel", w.cfg.PostChannel, "interest", a.Interest,
		"preview", truncate(draft, 80))
}

// appraisal is the structured verdict the model returns for one item.
type appraisal struct {
	Interest float64 `json:"interest"`
	Action   string  `json:"action"`
	Draft    string  `json:"draft"`
	Why      string  `json:"why"`
}

// appraise runs one model call primed with the agent's identity and parses the
// JSON verdict. A parse failure or model error is treated as "no engagement".
func (w *FeedWatcher) appraise(ctx context.Context, userPrompt string) (appraisal, bool) {
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	msgs := []core.Message{
		{Role: "system", Content: w.brain.SystemPrompt()},
		{Role: "user", Content: userPrompt},
	}
	resp, err := w.llm.Chat(callCtx, msgs, core.WithTemperature(0.3), core.WithMaxTokens(400))
	if err != nil || resp == nil {
		if ctx.Err() == nil {
			log.Warn("feed appraisal call failed", "error", err)
		}
		return appraisal{}, false
	}
	a, err := parseAppraisal(resp.Content)
	if err != nil {
		log.Debug("feed appraisal unparseable", "raw", truncate(resp.Content, 160))
		return appraisal{}, false
	}
	return a, true
}

// --- memory: engagement continuity + dedup ---------------------------------

func (w *FeedWatcher) memKey(suffix string) string { return "reef-eng:" + suffix }

// engaged reports whether the agent has already acted/decided on this target.
func (w *FeedWatcher) engaged(ctx context.Context, suffix string) bool {
	e, err := w.brain.Memory().Recall(ctx, w.memKey(suffix))
	return err == nil && e != nil
}

func (w *FeedWatcher) remember(ctx context.Context, suffix, outcome string) {
	_ = w.brain.Memory().Store(ctx, core.MemoryEntry{
		Key:        w.memKey(suffix),
		Value:      outcome,
		Tags:       []string{"reef", "feed-engagement"},
		Scope:      "system",
		Source:     "auto-learned",
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	})
}

// --- attention budget ------------------------------------------------------

// budget is a sliding-window cap on engagements per hour. It is the lever that
// makes the agent selective: when spent, it keeps observing but stops acting.
type budget struct {
	mu      sync.Mutex
	window  []time.Time
	perHour int
}

func (b *budget) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune()
	return len(b.window) < b.perHour
}

func (b *budget) record() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.window = append(b.window, time.Now())
}

func (b *budget) prune() {
	cutoff := time.Now().Add(-time.Hour)
	i := 0
	for i < len(b.window) && b.window[i].Before(cutoff) {
		i++
	}
	b.window = b.window[i:]
}

// --- helpers ---------------------------------------------------------------

// agentChainDepth counts how many consecutive agent-authored (non-owner)
// comments sit above c in the parent chain, i.e. how deep an agent<->agent
// exchange has gone. Owner ("owner"/"sourav") comments reset the count, since a
// human stepping in is not the runaway loop we guard against.
func agentChainDepth(c reef.Comment, byID map[string]reef.Comment, me string) int {
	depth := 0
	// The comments come from the hub as untrusted JSON; a malformed parent chain
	// could be cyclic (a->b->a). Track visited ids so a cycle stops the walk
	// instead of spinning forever and pinning the goroutine.
	visited := map[string]bool{c.ID: true}
	cur := c
	for cur.ParentCommentID != "" {
		if visited[cur.ParentCommentID] {
			break
		}
		parent, ok := byID[cur.ParentCommentID]
		if !ok || isOwner(parent.Author) {
			break
		}
		visited[cur.ParentCommentID] = true
		depth++
		cur = parent
	}
	return depth
}

func isOwner(author string) bool {
	return author == "owner" || author == "sourav"
}

// parseAppraisal extracts the JSON verdict from a model response, tolerating
// code fences or surrounding prose by slicing to the outermost braces.
func parseAppraisal(raw string) (appraisal, error) {
	s := strings.TrimSpace(raw)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return appraisal{}, fmt.Errorf("no json object in response")
	}
	var a appraisal
	if err := json.Unmarshal([]byte(s[start:end+1]), &a); err != nil {
		return appraisal{}, err
	}
	a.Action = strings.ToLower(strings.TrimSpace(a.Action))
	return a, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// sleepCtx sleeps for d or until ctx is cancelled; returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
