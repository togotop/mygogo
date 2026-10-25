// Command android-smoke exercises the mobile capabilities on Android. Each
// button runs one capability and prints its result, so every phase of
// docs/android-capability-plan.md can be seen working on a device.
//
// A few checks run on startup instead, because they need no tapping: the
// Wi-Fi name, the multicast lock, and a notification with progress and an
// action. Their results go to logcat as "smoke:" lines.
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type smoke struct {
	win   *mygo.Window
	lines []string
}

// add appends a line on the UI thread, which is where view reads it.
func (s *smoke) add(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	log.Printf("smoke: %s", line)
	if s.win == nil {
		s.lines = append(s.lines, line)
		return
	}
	s.win.Update(func() { s.lines = append(s.lines, line) })
}

func (s *smoke) run(name string, fn func() error) {
	go func() {
		err := fn()
		s.add("%s -> %v", name, err)
	}()
}

func (s *smoke) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(16).Gap(10).Children(func() {
		ui.Text(c, "Android capability smoke").FontSize(20).Bold()
		ui.Text(c, "Tap a row; the result appears below.").FontSize(12).TextColor(t.TextMuted)
		if ui.Button(c, "Haptics.Play(medium)").Clicked() {
			s.run("Haptics.Play(medium)", func() error { return mygo.Haptics.Play(mygo.HapticMedium) })
		}
		if ui.Button(c, "App.SetNotificationBadge(3)").Clicked() {
			s.run("SetNotificationBadge(3)", func() error { return mygo.App.SetNotificationBadge(3) })
		}
		if ui.Button(c, "App.SetStatusBar(dark)").Clicked() {
			s.run("SetStatusBar(dark)", func() error { return mygo.App.SetStatusBar(mygo.StatusBarDark, false) })
		}
		if ui.Button(c, "App.RegisterPushNotifications (FCM)").Clicked() {
			s.run("RegisterPush", func() error { return mygo.App.RegisterPushNotifications() })
		}
		if ui.Button(c, "Dialog.Photos(multiple)").Clicked() {
			go func() {
				paths, err := mygo.Dialog.Photos(mygo.PhotoDialogOptions{Multiple: true})
				s.add("Dialog.Photos(multiple) -> %d paths, err=%v", len(paths), err)
			}()
		}
		if ui.Button(c, "Dialog.Export(one file)").Clicked() {
			go func() {
				src, err := os.Executable()
				if err != nil {
					s.add("export: no executable: %v", err)
					return
				}
				ok, err := mygo.Dialog.Export(mygo.ExportDialogOptions{Files: []string{src}, Title: "app_process"})
				s.add("Dialog.Export(one) -> %v, err=%v", ok, err)
			}()
		}
		if ui.Button(c, "Network.WifiNetworks").Clicked() {
			go func() {
				nets, err := mygo.Network.WifiNetworks(10 * time.Second)
				s.add("Network.WifiNetworks -> %d networks, err=%v", len(nets), err)
			}()
		}
		if ui.Button(c, "Network.JoinWifi(AndroidWifi)").Clicked() {
			go func() {
				// 系统会弹「Connect to device」，用户点 CONNECT 才算数。
				err := mygo.Network.JoinWifi(mygo.WifiJoinOptions{SSID: "AndroidWifi", Timeout: 60 * time.Second})
				if err != nil {
					s.add("Network.JoinWifi -> %v", err)
					return
				}
				s.add("Network.JoinWifi -> joined, now %+v", mygo.Network.Wifi())
				s.add("Network.LeaveWifi -> %v", mygo.Network.LeaveWifi())
			}()
		}
		if ui.Button(c, "Dialog.Export(two files)").Clicked() {
			go func() {
				src, err := os.Executable()
				if err != nil {
					s.add("export: no executable: %v", err)
					return
				}
				ok, err := mygo.Dialog.Export(mygo.ExportDialogOptions{Files: []string{src, src}, Title: "two"})
				s.add("Dialog.Export(two) -> %v, err=%v", ok, err)
			}()
		}
		for _, line := range s.lines {
			ui.Text(c, line).FontSize(13)
		}
	})
}

// verifyLocal runs the checks that need no tapping: the Wi-Fi name, the
// multicast lock, and a notification carrying progress and an action.
func (s *smoke) verifyLocal() {
	s.add("wifi before -> %+v", mygo.Network.Wifi())
	if err := mygo.Network.Multicast(true); err != nil {
		s.add("Network.Multicast(true) -> %v", err)
	} else {
		s.add("Network.Multicast(true) -> held")
	}
	s.add("wifi with lock -> %+v", mygo.Network.Wifi())
	progress := 0.4
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:       "smoke-action",
		Title:    "Smoke: action and progress",
		Body:     "Press the button",
		Progress: &progress,
		Actions:  []mygo.NotificationAction{{ID: "ok", Label: "OK"}},
	})
	if err := n.Show(); err != nil {
		s.add("Notification.Show(action+progress) -> %v", err)
	} else {
		s.add("Notification.Show(action+progress) -> ok")
	}
	// Scheduled 20s ahead so the caller can kill the process before it fires:
	// what proves the alarm path is that the notification still arrives.
	delayed := mygo.NewNotification(mygo.NotificationOptions{
		ID:    "smoke-delayed",
		Title: "Smoke: delayed",
		Body:  "Scheduled 20 seconds ahead",
	})
	if err := delayed.Schedule(20 * time.Second); err != nil {
		s.add("Notification.Schedule(20s) -> %v", err)
	} else {
		s.add("Notification.Schedule(20s) -> ok")
	}
	if nets, err := mygo.Network.WifiNetworks(8 * time.Second); err != nil {
		s.add("Network.WifiNetworks -> %v", err)
	} else {
		s.add("Network.WifiNetworks -> %d networks", len(nets))
		for i, net := range nets {
			if i == 5 {
				break
			}
			log.Printf("smoke: wifi %q %s %d dBm %s", net.SSID, net.BSSID, net.RSSI, net.Security)
		}
	}
	s.add("NFC.Available -> %v", mygo.NFC.Available())
	if stop, err := mygo.NFC.Scan(func(tag mygo.NFCTag) {
		log.Printf("smoke: NFC %s %v writable=%v records=%+v", tag.ID, tag.Tech, tag.Writable, tag.Records)
	}); err != nil {
		s.add("NFC.Scan -> %v", err)
	} else {
		s.add("NFC.Scan -> started")
		_ = stop
	}

	if err := mygo.Bluetooth.Scan(2*time.Second, func(d mygo.BLEDevice) {
		log.Printf("smoke: BLE %q %s %d dBm", d.Name, d.Address, d.RSSI)
	}); err != nil {
		s.add("Bluetooth.Scan -> %v", err)
	} else {
		s.add("Bluetooth.Scan -> done")
	}
	// No peripheral is within reach of the emulator, so this exercises the
	// failure path: the call must come back rather than hang.
	if conn, err := mygo.Bluetooth.Connect("00:00:00:00:00:00", 5*time.Second); err != nil {
		s.add("Bluetooth.Connect(unreachable) -> %v", err)
	} else {
		s.add("Bluetooth.Connect(unreachable) -> connected, %d services", len(conn.Services()))
		_ = conn.Close()
	}

	if err := mygo.Speech.Speak("你好，这是语音合成测试", mygo.SpeakOptions{}); err != nil {
		s.add("Speech.Speak -> %v", err)
	} else {
		s.add("Speech.Speak -> ok")
	}
	// The engine starts in the background: the second call, once it has had
	// time, says whether this device has one at all.
	time.AfterFunc(2*time.Second, func() {
		if err := mygo.Speech.Speak("第二次", mygo.SpeakOptions{}); err != nil {
			log.Printf("smoke: Speech.Speak retry -> %v", err)
		} else {
			log.Printf("smoke: Speech.Speak retry -> ok")
		}
	})

	if err := mygo.Tile.Set(mygo.TileContent{Label: "回家", Subtitle: "场景", Active: false}); err != nil {
		s.add("Tile.Set -> %v", err)
	} else {
		s.add("Tile.Set -> ok")
	}
	mygo.Tile.OnClick(func() { s.add("Tile.OnClick") })

	if stop, err := mygo.MDNS.Advertise(mygo.MDNSService{
		Instance: "Smoke",
		Type:     "_mygo-smoke._tcp",
		Port:     9999,
		TXT:      []string{"v=1"},
	}); err != nil {
		s.add("MDNS.Advertise -> %v", err)
	} else {
		s.add("MDNS.Advertise -> ok")
		_ = stop
	}
	if _, err := mygo.MDNS.Browse("_mygo-smoke._tcp", func(svc mygo.MDNSService) {
		log.Printf("smoke: MDNS found %q at %v:%d", svc.Instance, svc.Addrs, svc.Port)
	}); err != nil {
		s.add("MDNS.Browse -> %v", err)
	} else {
		s.add("MDNS.Browse -> ok")
	}

	if v := mygo.Audio.Volume(); true {
		s.add("Audio.Volume -> %d%%, muted=%v, ringer=%s", v.Percent, v.Muted, v.Ringer)
	}
	if err := mygo.Audio.SetVolume(60); err != nil {
		s.add("Audio.SetVolume(60) -> %v", err)
	} else {
		s.add("Audio.SetVolume(60) -> ok, now %d%%", mygo.Audio.Volume().Percent)
	}
	if err := mygo.Audio.Adjust(-2); err != nil {
		s.add("Audio.Adjust(-2) -> %v", err)
	}
	if err := mygo.Audio.Press(mygo.KeyPlayPause); err != nil {
		s.add("Audio.Press(play-pause) -> %v", err)
	} else {
		s.add("Audio.Press(play-pause) -> ok")
	}
	if err := mygo.Audio.SetRingerMode(mygo.RingerVibrate); err != nil {
		s.add("Audio.SetRingerMode(vibrate) -> %v", err)
	} else {
		s.add("Audio.SetRingerMode(vibrate) -> ok, ringer=%s", mygo.Audio.Volume().Ringer)
		_ = mygo.Audio.SetRingerMode(mygo.RingerNormal)
	}
	if err := mygo.Audio.SetRingerMode(mygo.RingerSilent); err != nil {
		s.add("Audio.SetRingerMode(silent) -> %v (allowed=%v)", err, mygo.Audio.SilenceAllowed())
	} else {
		s.add("Audio.SetRingerMode(silent) -> ok")
		_ = mygo.Audio.SetRingerMode(mygo.RingerNormal)
	}

	// SSDP: 本机先起一个应答器（模拟一台 UPnP 设备），再搜索，验证收发与解析。
	stopSSDP := startSSDPResponder()
	defer stopSSDP()
	if err := mygo.SSDP.Search("ssdp:all", 5*time.Second, func(d mygo.SSDPDevice) {
		log.Printf("smoke: SSDP %q from %v usn=%s", d.Server, d.Addr, d.USN)
	}); err != nil {
		s.add("SSDP.Search -> %v", err)
	} else {
		s.add("SSDP.Search -> done")
	}

	if proxy := mygo.Network.Proxy(); proxy.Host == "" {
		s.add("Network.Proxy -> direct")
	} else {
		s.add("Network.Proxy -> %s:%d", proxy.Host, proxy.Port)
	}

	if exempt, err := mygo.Power.BatteryExempt(); err != nil {
		s.add("Power.BatteryExempt -> %v", err)
	} else {
		s.add("Power.BatteryExempt -> %v", exempt)
	}
	if err := mygo.Power.RequestBatteryExemption(); err != nil {
		s.add("Power.RequestBatteryExemption -> %v", err)
	} else {
		s.add("Power.RequestBatteryExemption -> ok")
	}

	if err := mygo.Widget.Set(mygo.WidgetContent{
		Title: "智能家居",
		Rows: []mygo.WidgetRow{
			{ID: "lamp", Label: "客厅灯", Subtitle: "关"},
			{ID: "ac", Label: "空调", Subtitle: "26°C"},
			{ID: "scene", Label: "回家模式", Subtitle: "执行"},
		},
	}); err != nil {
		s.add("Widget.Set -> %v", err)
	} else {
		s.add("Widget.Set -> ok")
	}
	mygo.Widget.OnAction(func(id string) { s.add("Widget.OnAction -> %q", id) })

	if pos, err := mygo.Geolocation.Current(); err != nil {
		s.add("Geolocation.Current -> %v", err)
	} else {
		s.add("Geolocation.Current -> %.5f,%.5f ±%.0fm at %s", pos.Latitude, pos.Longitude, pos.Accuracy, pos.Time.Format(time.RFC3339))
	}
	// A foreground service keeps the process alive while it is backgrounded.
	// Released 20s later, so dumpsys can be read in between.
	if release, err := mygo.App.KeepRunning("Smoke: foreground service"); err != nil {
		s.add("App.KeepRunning -> %v", err)
	} else {
		s.add("App.KeepRunning -> ok")
		// Released 20s later in a timer, so the service can be seen holding the
		// process while it is backgrounded, then seen going away.
		time.AfterFunc(20*time.Second, func() {
			log.Printf("smoke: App.KeepRunning releasing")
			release()
			log.Printf("smoke: App.KeepRunning released")
		})
	}
}

// startSSDPResponder answers M-SEARCH with one made-up UPnP device, so the
// search is verified end to end on a device with nothing else around it.
func startSSDPResponder() (stop func()) {
	group := &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 1900}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		log.Printf("smoke: SSDP responder: %v", err)
		return func() {}
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// 只应答搜索，忽略别的设备自己的广播。
			if !strings.HasPrefix(string(buf[:n]), "M-SEARCH") {
				continue
			}
			reply := "HTTP/1.1 200 OK\r\n" +
				"CACHE-CONTROL: max-age=60\r\n" +
				"ST: urn:schemas-upnp-org:device:basic:1\r\n" +
				"USN: uuid:smoke-0001::urn:schemas-upnp-org:device:basic:1\r\n" +
				"SERVER: mygo-smoke/1.0 UPnP/1.1\r\n" +
				"LOCATION: http://127.0.0.1:9999/description.xml\r\n" +
				"\r\n"
			_, _ = conn.WriteToUDP([]byte(reply), from)
		}
	}()
	return func() { conn.Close() }
}

func main() { run() }

func run() {
	s := &smoke{}
	mygo.App.OnNotification(func(e mygo.NotificationEvent) {
		s.add("OnNotification id=%q clicked=%v action=%q", e.ID, e.Clicked, e.Action)
	})
	mygo.App.OnPushToken(func(tok string) { s.add("push token: %q", tok) })
	mygo.App.OnPushRegistrationError(func(err error) { s.add("push error: %v", err) })
	mygo.App.WhenReady(func() {
		s.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Android smoke", Width: 520, Height: 720, Content: ui.View(s.view),
		})
		// Phase 0: this call must answer instead of leaving its caller waiting.
		go func() {
			err := mygo.App.SetNotificationBadge(3)
			s.add("startup SetNotificationBadge(3) -> %v", err)
		}()
		go s.verifyLocal()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
