// Command bridgehost plays the trusted host around the bridge library so an
// agent can drive it one call per process, the way a restarted host would.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KellyBennett/dot-bridge/bridge"
)

const (
	principal   = "verify-principal"
	environment = "personal"
	project     = "personal-demo"
	workspace   = "synthetic-workspace"
	designText  = "Synthetic design"
)

var epoch = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)

type hostState struct {
	Scenario    string    `json:"scenario"`
	Now         time.Time `json:"now"`
	DenyExports bool      `json:"deny_exports"`
}

type host struct {
	run   string
	state hostState
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: bridgehost <init|envelope|review|approve|dispatch|tick|advance> [args]"))
	}
	run := os.Getenv("BRIDGE_RUN")
	if run == "" {
		fail(errors.New("BRIDGE_RUN is not set"))
	}
	h := &host{run: run}
	command, args := os.Args[1], os.Args[2:]
	if command == "init" {
		exit(h.init(args))
	}
	if err := h.load(); err != nil {
		fail(err)
	}
	exit(h.perform(command, args))
}

func (h *host) perform(command string, args []string) error {
	switch command {
	case "envelope":
		return h.envelope(args)
	case "review":
		return h.withBroker(func(b *bridge.Broker, _ *bridge.Journal) error { return h.review(b, args) })
	case "approve":
		return h.withBroker(func(b *bridge.Broker, _ *bridge.Journal) error { return h.approve(b, args) })
	case "dispatch":
		return h.withBroker(func(b *bridge.Broker, _ *bridge.Journal) error { return h.dispatch(b, args) })
	case "tick":
		return h.withBroker(h.tick)
	case "advance":
		return h.advance(args)
	}
	return fmt.Errorf("unknown command %q", command)
}

func (h *host) init(args []string) error {
	h.state = hostState{Scenario: "success", Now: epoch}
	for _, arg := range args {
		if arg == "--deny-exports" {
			h.state.DenyExports = true
		} else {
			h.state.Scenario = arg
		}
	}
	if _, err := h.profileRevision(); err != nil {
		return fmt.Errorf("scenario %q: %w", h.state.Scenario, err)
	}
	if err := os.MkdirAll(h.instance(), 0o700); err != nil {
		return err
	}
	if err := h.save(); err != nil {
		return err
	}
	return h.withBroker(func(*bridge.Broker, *bridge.Journal) error {
		fmt.Printf("bridgehost ready scenario=%s now=%s\n", h.state.Scenario, h.state.Now.Format(time.RFC3339))
		return nil
	})
}

func (h *host) envelope(args []string) error {
	prompt := "Synthetic task\nExact bytes. "
	if len(args) > 0 {
		prompt = args[0]
	}
	adapterProfile, err := h.profileRevision()
	if err != nil {
		return err
	}
	tasks := h.taskConfig(adapterProfile)
	return emit(bridge.TaskEnvelope{CanonicalVersion: "1", TaskID: "00000000-0000-4000-8000-000000000002",
		ProjectID: project, Environment: environment, WorkspaceID: tasks.WorkspaceID, WorkspaceSnapshot: tasks.Snapshot,
		Prompt: prompt, PromptDigest: digest(prompt),
		Specs:          []bridge.SpecReference{{DocumentID: "design", Revision: digest(designText)}},
		AgentProfileID: "stub-v1", ProfileRevision: adapterProfile,
		AllowedEffects:       []string{"edit_workspace", "run_approved_verification"},
		ExportPolicyRevision: tasks.ExportPolicyRevision, PolicyRevision: digest("policy-v1"),
		AcceptanceCriteria: []string{"Review the scripted synthetic diff"}, Simulated: true})
}

func (h *host) review(b *bridge.Broker, args []string) error {
	raw, err := input(args)
	if err != nil {
		return err
	}
	review, err := b.ReviewTask(raw)
	if err != nil {
		return err
	}
	return emit(review)
}

func (h *host) approve(b *bridge.Broker, args []string) error {
	raw, err := input(args)
	if err != nil {
		return err
	}
	var review bridge.ApprovalReview
	if err := json.Unmarshal(raw, &review); err != nil {
		return err
	}
	approval, err := b.IssueApproval(context.Background(), review)
	if err != nil {
		return err
	}
	return emit(approval)
}

func (h *host) dispatch(b *bridge.Broker, args []string) error {
	raw, err := input(args)
	if err != nil {
		return err
	}
	response, err := b.Dispatch(context.Background(), raw, &bridge.Identity{PrincipalID: principal, Environment: environment})
	if err != nil {
		return err
	}
	return emit(response)
}

func (h *host) tick(b *bridge.Broker, journal *bridge.Journal) error {
	adapter, err := bridge.NewStubAdapter(journal, h.state.Scenario)
	if err != nil {
		return err
	}
	worker, err := b.NewWorker(adapter)
	if err != nil {
		return err
	}
	response, err := worker.Tick(context.Background())
	if err != nil {
		return err
	}
	return emit(response)
}

func (h *host) advance(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: bridgehost advance <duration>")
	}
	step, err := time.ParseDuration(args[0])
	if err != nil || step <= 0 {
		return fmt.Errorf("invalid positive duration %q", args[0])
	}
	h.state.Now = h.state.Now.Add(step)
	if err := h.save(); err != nil {
		return err
	}
	fmt.Println(h.state.Now.Format(time.RFC3339))
	return nil
}

func (h *host) withBroker(use func(*bridge.Broker, *bridge.Journal) error) error {
	journal, err := bridge.OpenJournal(filepath.Join(h.instance(), "journal.sqlite3"))
	if err != nil {
		return err
	}
	defer journal.Close()
	b, err := bridge.NewBroker(journal, h.config())
	if err != nil {
		return err
	}
	return use(b, journal)
}

func (h *host) config() bridge.Config {
	adapterProfile, _ := h.profileRevision()
	now := h.state.Now
	c := bridge.Config{
		Grant: bridge.Grant{PrincipalID: principal, Environment: environment, ProjectID: project,
			Operations:  []string{"read_draft", "write_draft", "submit_task", "read_run", "cancel_run", "read_pr"},
			DocumentIDs: []string{"design"}, PolicyRevision: digest("policy-v1"),
			ExpiresAt: epoch.Add(24 * time.Hour), Enabled: true},
		Documents: map[string]string{"design": designText},
		Clock:     func() time.Time { return now },
		Tasks:     h.taskConfig(adapterProfile),
	}
	if !h.state.DenyExports {
		c.ExportCheck = func(string) (bool, error) { return true, nil }
	}
	return c
}

func (h *host) taskConfig(adapterProfile string) *bridge.TaskConfig {
	return &bridge.TaskConfig{WorkspaceID: workspace,
		Snapshot:        bridge.WorkspaceSnapshot{BaseCommit: strings.Repeat("a", 40), DirtyManifestDigest: digest("clean")},
		ProfileRevision: adapterProfile, ExportPolicyRevision: digest("synthetic-export")}
}

// The unopened journal only satisfies NewStubAdapter's nil check; Capabilities
// never touches storage.
func (h *host) profileRevision() (string, error) {
	adapter, err := bridge.NewStubAdapter(&bridge.Journal{}, h.state.Scenario)
	if err != nil {
		return "", err
	}
	return adapter.Capabilities().ProfileRevision, nil
}

func (h *host) instance() string  { return filepath.Join(h.run, "instance") }
func (h *host) statePath() string { return filepath.Join(h.instance(), "host.json") }

func (h *host) load() error {
	raw, err := os.ReadFile(h.statePath())
	if err != nil {
		return fmt.Errorf("no instance at %s; run launch.sh first: %w", h.run, err)
	}
	return json.Unmarshal(raw, &h.state)
}

func (h *host) save() error {
	raw, err := json.MarshalIndent(h.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.statePath(), raw, 0o600)
}

func input(args []string) ([]byte, error) {
	if len(args) == 0 || args[0] == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(args[0])
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func emit(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func exit(err error) {
	if err != nil {
		fail(err)
	}
	os.Exit(0)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bridgehost:", err)
	os.Exit(2)
}
