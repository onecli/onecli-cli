package main

// OrgCmd is the `onecli org` command group for organization-scoped operations.
type OrgCmd struct {
	Secrets     OrgSecretsCmd     `cmd:"" help:"Manage org-scoped secrets."`
	Rules       OrgRulesCmd       `cmd:"" help:"Manage org-scoped policy rules."`
	Connections OrgConnectionsCmd `cmd:"" help:"Manage org-scoped connections."`
	Apps        OrgAppsCmd        `cmd:"" help:"Manage org-scoped app configuration."`
}
