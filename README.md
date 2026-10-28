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
the same app:

```go
import (
	gessoplugin "github.com/Elagoht/collage-gesso"
	"github.com/Elagoht/gesso"
)

api, err := gesso.New(gesso.Config{BasePath: "/api"}, notes.New(db))
if err != nil {
	return err
}
api.OnShutdown(func(context.Context) error { return db.Close() })

app, err := collage.New(&collage.Config{
	Plugins: []collage.Plugin{gessoplugin.New(gessoplugin.Options{App: api})},
})
app.Handle("/api/", api)
```

Requires collage v0.80.0 and gesso v0.5.0 or later. There is nothing in
`plugins-config.json`; without `Options.App`, startup fails.

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
