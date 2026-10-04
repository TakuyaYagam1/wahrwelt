package tui

import (
	"github.com/charmbracelet/huh"
)

func editServices(s *session) error {
	return newForm(serviceFields(s)...).Run()
}

func serviceFields(s *session) []huh.Field {
	return []huh.Field{
		huh.NewConfirm().
			Title("Enable OmniRouter").
			Description("Builds/enables the local OmniRouter package and module used by this config.").
			Value(&s.state.Features.OmniRouter),
		huh.NewConfirm().
			Title("Enable Portainer").
			Description("Enables a localhost container management UI connected to the selected per-user engine. Portainer starts at https://127.0.0.1:9443 - set the admin password on first visit.").
			Value(&s.state.Features.Portainer),
		huh.NewConfirm().
			Title("Enable Observability").
			Description("Enables the localhost Grafana/Prometheus/Loki stack. Grafana starts at http://127.0.0.1:3010 with initial admin/admin login.").
			Value(&s.state.Features.Observability),
	}
}
