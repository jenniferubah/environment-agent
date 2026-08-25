// Package shared holds agent-level settings passed to embedded OpenShift SPs.
package shared

import (
	agentconfig "github.com/dcm-project/environment-agent/internal/config"
)

// Config carries deployment settings common to all embedded SPs on this agent.
type Config struct {
	MessagingURL string
	Kubeconfig   string
}

// FromAgent extracts embedded SP shared settings from agent configuration.
func FromAgent(cfg *agentconfig.Config) Config {
	if cfg == nil {
		return Config{}
	}
	return Config{
		MessagingURL: cfg.Messaging.URL,
		Kubeconfig:   cfg.Agent.Kubeconfig,
	}
}
