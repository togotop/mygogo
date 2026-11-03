# NetPath

The net path plugin tells how the device reaches the network, and hears it
change: whether it is online, over Wi-Fi or cellular, and whether that link
costs the user data. An app that syncs when it can, or holds back on a metered
link, asks it before it starts.

```go
import "github.com/egoist/mygo/plugins/netpath"

path, err := netpath.Current(ctx)
if err != nil {
	return err
}
if path.Online && !path.Expensive {
	sync()
}

stop, err := netpath.Watch(ctx, func(p netpath.Path) { update(p) })
if err != nil {
	return err
}
defer stop()
```

| Function | What it does |
| --- | --- |
| `Current(ctx)` | How the device reaches the network now. |
| `Watch(ctx, fn)` | Reports each change to `fn` until `Stop`. |

A `Path` has `Online` (the device reaches the internet, not merely a network: a
Wi-Fi that asks for a sign-in is not), `Interface` (`wifi`, `cellular`,
`ethernet`, `vpn`, `other`, or `none` while offline), `Expensive` (a link the
user pays for, as a metered cellular one) and `Constrained` (the system asking
apps to go easy on it). `Known` says whether the system could tell at all.

Android reads ConnectivityManager's default network and its capabilities, and
watches them with a network callback; the plugin declares
`ACCESS_NETWORK_STATE`, which apps have anyway. For the coarser question every
platform answers, `mygo.Network.Status` is enough: online, and metered.

iOS has no Swift for it yet.
