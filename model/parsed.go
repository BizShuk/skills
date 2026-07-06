package model

// Parsed is the combined output of Scan: every local plugin discovered on
// disk under base plus every remote plugin declared in either manifest.
type Parsed struct {
	Locals  []LocalPlugin
	Remotes []RemotePlugin
}
