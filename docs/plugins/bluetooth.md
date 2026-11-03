# Bluetooth

The Bluetooth plugin is the device's Bluetooth LE radio, for an app that acts
as a remote control or reads a sensor: a scan reports the devices in reach, and
a connection reads, writes and subscribes to the characteristics of one of
them. Android's own stack (LeScanner and BluetoothGatt) does the radio work;
the plugin's Kotlin is what mygo build compiles into the app.

```go
import "github.com/egoist/mygo/plugins/bluetooth"

scan, err := bluetooth.Scan(ctx, "", func(d bluetooth.Device) {
	log.Printf("%s %s %d dBm", d.Address, d.Name, d.RSSI)
})
if err != nil {
	return err
}
defer scan.Stop()

conn, err := bluetooth.Connect(ctx, address)
if err != nil {
	return err
}
defer conn.Close()
for _, s := range conn.Services() {
	log.Println(s.UUID, s.Characteristics)
}
value, err := conn.Read(ctx, "2a37") // the heart rate measurement
```

## What it does

| Function | What it does |
| --- | --- |
| `Available(ctx)` | Whether the device has a Bluetooth LE radio that is on. |
| `Scan(ctx, service, fn)` | Looks for devices, reporting each to `fn` until `Stop`. `service` narrows the scan to what advertises it, in the short form (`180d`) or the full UUID. |
| `Connect(ctx, address)` | Connects to a device a scan reported, and discovers its services. |
| `Conn.Read(ctx, characteristic)` | Reads a characteristic's value. |
| `Conn.Write(ctx, characteristic, value, withResponse)` | Writes one, acknowledged by the device or not. |
| `Conn.Notify(ctx, characteristic, fn)` | Subscribes to its notifications, until the returned function unsubscribes. |
| `Conn.Close()` | Drops the connection. `Done()` closes with it. |

`ErrDisconnected`, `ErrTimeout` and `ErrCanceled` say why a call failed: the
link dropped, the device did not answer, or the user denied the permission.

## Permissions

The scan asks for the system's permission to see devices the first time: on
Android 12 and later `BLUETOOTH_SCAN` and `BLUETOOTH_CONNECT`, before it the
location one. The plugin's `mygo-plugin.json` declares them, so the app's
manifest has them; the user is asked when a scan or a connection starts.

iOS has no Swift for it yet: its functions report `errors.ErrUnsupported`
there.
