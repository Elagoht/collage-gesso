// Package gesso is a collage plugin that runs a gesso application's lifecycle:
// the app's modules (a queue worker, a warmed cache) start when collage starts
// serving, and stop, followed by the app's OnShutdown hooks, after collage's
// server has stopped and no request can reach them.
//
// The plugin does not mount the app. An API is mounted with the application's
// own Handle, which keeps collage's and the plugins' middleware off it:
//
//	api, err := gesso.New(gesso.Config{BasePath: "/api"}, notes.New(db))
//	…
//	app.Handle("/api/", api)
//
// and the plugin, in plugins.go, is given the same app — collage plugin add
// imports this package as gesso, so the framework takes another name there:
//
//	func plugins(api *gessoapp.App) []collage.Plugin {
//		return []collage.Plugin{gesso.New(gesso.Options{App: api})}
//	}
//
// A page or an action calls the API in the same process through
// gesso.NewLocal(api), which runs the call through the app's whole pipeline
// without the network; with the app in a process of its own, gesso.NewRemote
// takes its place and nothing else changes.
//
// Nothing starts for a static build, or when the collage CLI runs the program
// to ask it something: Init only checks the configuration. If the app fails to
// start, the failure is logged and the app answers every request 503.
package gesso

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/gesso"
)

// Name is the plugin's name.
const Name = "elagoht/gesso"

const version = "0.1.1"

// Options configures the plugin. It has nothing in the application's plugin
// configuration: the app is given in code.
type Options struct {
	App *gesso.App `json:"-"` // the gesso app whose lifecycle collage runs; required
}

// Plugin runs a gesso app's lifecycle.
type Plugin struct {
	opts Options
	log  *slog.Logger
}

var (
	_ collage.Plugin    = (*Plugin)(nil)
	_ collage.ServeHook = (*Plugin)(nil)
)

// New returns the plugin for opts.App.
func New(opts Options) *Plugin { return &Plugin{opts: opts} }

// Name returns Name.
func (p *Plugin) Name() string { return Name }

// Version returns the plugin's version.
func (p *Plugin) Version() string { return version }

// Init checks that an app was given. It starts nothing: a static build and the
// collage CLI's questions initialise plugins too.
func (p *Plugin) Init(_ context.Context, host collage.Host) error {
	if p.opts.App == nil {
		return errors.New("gesso: Options.App is nil: give the plugin the gesso app collage mounts")
	}
	if _, err := collage.PluginConfig(host, struct{}{}); err != nil {
		return err
	}
	p.log = host.Logger()
	return nil
}

// OnServe starts the app's modules, after the port is bound and before the first
// request is served. A failure is logged; the app then answers 503.
func (p *Plugin) OnServe(ctx context.Context) {
	if err := p.opts.App.Start(ctx); err != nil {
		p.log.Error("gesso: the app did not start; it answers 503", "error", err)
	}
}

// Shutdown stops the app's modules and runs its OnShutdown hooks. Collage calls
// it after its server has stopped, so no request meets a stopped module. It runs
// the hooks even when the app never started — after a build, say — so a
// database the app's hooks close is closed.
func (p *Plugin) Shutdown(ctx context.Context) error {
	if p.opts.App == nil {
		return nil
	}
	return p.opts.App.Shutdown(ctx)
}
