# elagoht/gesso

A collage plugin that runs a [gesso](https://github.com/Elagoht/gesso) app's
lifecycle when collage serves it in the same process: the app's modules — a
queue worker, a warmed cache — start when collage starts serving, and stop,
followed by the app's `OnShutdown` hooks, after collage's server has stopped and
no request can reach them.

```sh
collage plugin add elagoht/gesso
```

The plugin does not mount the app. Mount it with the application's own `Handle`,
which keeps collage's and the plugins' middleware off an API, and give the plugin
the same app. In an application `collage new` made, build the app in `newApp`,
before `collage.New`, and hand it to `plugins`:

```go
// main.go
func newApp(env collage.Env) (*collage.App, error) {
	api, err := gesso.New(gesso.Config{BasePath: "/api"}, notes.New(notes.Deps{DB: db}))
	if err != nil {
		return nil, err
	}
	…
	app, err := collage.New(&collage.Config{
		…
		Plugins: plugins(api),
	})
	if err != nil {
		return nil, err
	}
	if err := app.Handle("/api/", api); err != nil {
		return nil, err
	}
	…
}
```

`collage plugin add` imports this plugin as `gesso`, so in `plugins.go` the
framework takes another name. `plugins` may take arguments; `collage plugin add`
and `del` still edit its list:

```go
// plugins.go
import (
	"github.com/Elagoht/collage/pkg/collage"
	gessoapp "github.com/Elagoht/gesso"

	gesso "github.com/Elagoht/collage-gesso"
)

func plugins(api *gessoapp.App) []collage.Plugin {
	return []collage.Plugin{
		gesso.New(gesso.Options{App: api}),
	}
}
```

Requires collage v0.80.0 and gesso v0.5.0 or later. There is nothing in
`plugins-config.json`; without `Options.App`, startup fails. Close what the app
holds — a database — in its `OnShutdown` hooks: the plugin runs them last.

## Starting from scratch

gesso's CLI writes the API side into the site:

```sh
gesso init -dialect sqlite      # internal/api: database, migrations, OpenAPI
gesso add module notes -fields title:string,body:text
collage plugin add elagoht/gesso
```

`api.New(ctx, api.Config{BasePath: "/api", Database: …})` is then the app the
two blocks above build and hand on; gesso's
[With collage](https://github.com/Elagoht/gesso/blob/main/docs/collage.md) guide
walks through it.

## Calling the API from pages

A page's data handler or an action calls the API through an endpoint both sides
declare, in the same process:

```go
local := gesso.NewLocal(api)
note, err := notes.GetNote.Call(ctx, local, notes.GetIn{Slug: slug}, gesso.Bearer(token))
```

The call goes through the app's whole pipeline — authentication, guards,
validation, interceptors — and comes back as JSON or a problem, exactly as over
the network; it only never leaves memory. When the API moves to a process of its
own, `gesso.NewRemote("http://api.internal:8081/api", nil)` takes the place of
`NewLocal`, the plugin is dropped (the API's own `Run` handles its lifecycle),
and nothing else changes.

## When things start

- `Init` only checks that an app was given. A static build and the collage CLI's
  questions (`collage routes`, `collage check`) initialise plugins too, and must
  not start a queue worker.
- The app starts once the server has bound its port, before it serves.
- If it fails to start, the failure is logged and the app answers every request
  `503` with the problem code `unavailable`: its handlers would meet modules that
  are not running.
- On shutdown collage drains and stops its server first, so a request in flight
  is answered before any module stops. Then the modules stop in reverse order,
  then the app's `OnShutdown` hooks run — also when the app never started, so a
  database they close is closed after a build too.

In tests built with `collagetest`, nothing serves, so the app is not started:
handlers answer, and a worker's jobs run only when the test calls it.
