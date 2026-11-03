# Audio

The audio plugin is the phone's media volume, its ringer and the transport
keys of whatever is playing: what a control app needs to say "turn it down",
and what a player needs to say "pause". It is AudioManager's STREAM_MUSIC on
Android, so the alarm and the ringer keep their own levels.

```go
import "github.com/egoist/mygo/plugins/audio"

vol, err := audio.Current(ctx)
if err != nil {
	return err
}
log.Printf("%d%% (muted %v, ringer %s)", vol.Percent, vol.Muted, vol.Ringer)

err = audio.Adjust(ctx, -1)                 // one press of the volume key
err = audio.Set(ctx, 40)                    // 40%, on the media stream only
err = audio.Press(ctx, audio.KeyPlayPause)  // a media button
```

| Function | What it does |
| --- | --- |
| `Available(ctx)` | Whether the device has media controls to read and move. |
| `Current(ctx)` | The media volume: `Percent`, `Muted`, and `Ringer`. |
| `Set(ctx, percent)` | Sets the media volume, 0 to 100, and unmutes a muted stream. |
| `Adjust(ctx, steps)` | Moves it by steps, which may be negative, one being what a volume key moves. |
| `SetMuted(ctx, on)` | Mutes or unmutes the media stream, which keeps the volume it had. |
| `SetRinger(ctx, mode)` | `RingerNormal`, `RingerVibrate` or `RingerSilent`. |
| `SilenceAllowed(ctx)` | Whether `RingerSilent` will work: the user granted the do not disturb access. |
| `OpenSilenceSettings(ctx)` | Shows the system page where that access is granted. |
| `Press(ctx, key)` | Sends a transport key: `KeyPlayPause`, `KeyPlay`, `KeyPause`, `KeyNext`, `KeyPrevious`, `KeyStop`, `KeyForward`, `KeyRewind`. |

Silent is the mode the system guards: Android 6 and later want the
notification policy (do not disturb) access for it, and without it
`SetRinger(RingerSilent)` answers `ErrDndAccess`. The app cannot grant itself
the access; `OpenSilenceSettings` shows the page where the user does.

The plugin declares `ACCESS_NOTIFICATION_POLICY` (which the settings page
needs to list the app) and adds no other permission: moving the media volume
and sending media buttons need none.

Android only: a desktop's volume is the system's own, and iOS has no Swift for
it yet.
