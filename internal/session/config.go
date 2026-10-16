package session

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"aeswibon.com/github/gitopsctl/internal/policy"
	"sigs.k8s.io/yaml"
)

const (
	defaultTTL = 30 * time.Minute
	maxTTL     = 4 * time.Hour
)

var agentIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,18}[a-z0-9])?$`)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"30m\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

type Agent struct {
	ID          string            `json:"id"`
	Cluster     string            `json:"cluster"`
	Repos       []string          `json:"repos"`
	MaxSessions int               `json:"max_sessions"`
	DefaultTTL  Duration          `json:"default_ttl"`
	MaxTTL      Duration          `json:"max_ttl"`
	Quota       map[string]string `json:"quota"`
}

type Config struct {
	Policy *policy.Policy `json:"policy"`
	Agents []Agent        `json:"agents"`
}

func LoadConfig(file string) (*Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read agents file: %w", err)
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.UnmarshalStrict(data, cfg); err != nil {
		return nil, fmt.Errorf("parse agents file: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Policy == nil {
		return fmt.Errorf("invalid agents file: policy is required")
	}
	if err := c.Policy.Validate(); err != nil {
		return err
	}
	if len(c.Agents) == 0 {
		return fmt.Errorf("invalid agents file: at least one agent is required")
	}
	seen := map[string]bool{}
	for i := range c.Agents {
		agent := &c.Agents[i]
		if !agentIDPattern.MatchString(agent.ID) {
			return fmt.Errorf("invalid agent id %q: use 1-20 lowercase letters, digits or dashes", agent.ID)
		}
		if seen[agent.ID] {
			return fmt.Errorf("duplicate agent id %q", agent.ID)
		}
		seen[agent.ID] = true
		if agent.Cluster == "" {
			return fmt.Errorf("agent %q: cluster is required", agent.ID)
		}
		if len(agent.Repos) == 0 {
			return fmt.Errorf("agent %q: repos must not be empty", agent.ID)
		}
		for _, repo := range agent.Repos {
			if strings.TrimSpace(repo) == "" {
				return fmt.Errorf("agent %q: empty repo entry", agent.ID)
			}
		}
		if agent.MaxSessions <= 0 {
			return fmt.Errorf("agent %q: max_sessions must be positive", agent.ID)
		}
		if agent.DefaultTTL == 0 {
			agent.DefaultTTL = Duration(defaultTTL)
		}
		if agent.MaxTTL == 0 {
			agent.MaxTTL = Duration(maxTTL)
		}
		if agent.DefaultTTL < 0 || agent.DefaultTTL > agent.MaxTTL {
			return fmt.Errorf("agent %q: default_ttl must be positive and not exceed max_ttl", agent.ID)
		}
		if sample := NamespaceName(agent.ID, "00000000"); !c.Policy.NamespaceAllowed(sample) {
			return fmt.Errorf("agent %q: session namespaces such as %q are not permitted by the policy", agent.ID, sample)
		}
	}
	return nil
}

func (c *Config) Agent(id string) (*Agent, bool) {
	for i := range c.Agents {
		if c.Agents[i].ID == id {
			return &c.Agents[i], true
		}
	}
	return nil, false
}

func NamespaceName(agentID, sessionID string) string {
	return "agent-" + agentID + "-" + sessionID
}

func (a *Agent) allowsRepo(repo string) bool {
	for _, allowed := range a.Repos {
		if repo == allowed || (strings.HasSuffix(allowed, "/") && strings.HasPrefix(repo, allowed)) {
			return true
		}
	}
	return false
}
