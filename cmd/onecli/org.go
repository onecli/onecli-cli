package main

// OrgCmd is the `onecli org` command group for organization-scoped operations.
type OrgCmd struct {
	Secrets     OrgSecretsCmd     `cmd:"" help:"Manage org-scoped secrets."`
	Rules       OrgRulesCmd       `cmd:"" help:"Manage legacy org policy rules (cloud deployments reject writes — see 'onecli org policy')."`
	Policy      OrgPolicyCmd      `cmd:"" help:"Manage org policy rules on the policy engine (draft → publish)."`
	Connections OrgConnectionsCmd `cmd:"" help:"Manage org-scoped connections."`
	Apps        OrgAppsCmd        `cmd:"" help:"Manage org-scoped app configuration."`
	Settings    OrgSettingsCmd    `cmd:"" help:"RETIRED — updated servers answer 410 Gone. The allow/deny posture is the org Default Rule ('org policy default')."`
}
