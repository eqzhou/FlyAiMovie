package httpapi

// BuildRevision identifies the commit this binary was built from. It is set at
// link time by scripts/dev-up.sh (and any other build entry point) via
// -ldflags "-X .../internal/httpapi.BuildRevision=<sha>".
//
// It exists because PM2 runs a copy of the server staged under
// ~/.local/share/flyaimovie rather than the checkout: `pm2 restart` reruns
// whatever binary is already staged, so a stale deploy is otherwise invisible.
// /api/v1/health reports this value, which makes the drift checkable.
var BuildRevision = "unknown"
