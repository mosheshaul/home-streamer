# Home Streamer

Watch the videos on your Windows PC on an Android TV, phone or tablet.

- **`server/`**: a small Windows program (`HomeStreamer.exe`, written in Go) that shares your video folders.
- **`android-tv/`**: the Android app for TVs, phones and tablets (Kotlin, Compose for TV, Media3/ExoPlayer).

## What it does

- Browse your folders with the TV remote or by touch, with "Continue watching" and resume.
- Plays **MKV and MP4** (also M4V, MOV, WebM, AVI, TS) directly from the PC with no conversion, so seeking is instant and the PC barely works.
- **Subtitles:**
  - Embedded subtitle tracks inside MKV/MP4 (SRT, SSA/ASS, PGS, and more).
  - **SRT files next to the video**: `Movie.srt`, `Movie.he.srt`, `Movie.heb.srt`, `Movie.en.forced.srt`, and so on. If a folder holds a single video, any `.srt` in it is used, and so are files in a `Subs` or `Subtitles` subfolder.
  - Old Hebrew subtitles saved as Windows-1255 (and other legacy encodings, or UTF-16) are converted, so they never show as gibberish. Languages are detected from the file name, or from the text itself when the name doesn't say.
  - The app remembers the subtitle language you last picked and selects it automatically next time. You can also set the language and text size in **Settings**.
- Audio formats many TVs can't decode (DTS, TrueHD, and others) are handled by a bundled FFmpeg software decoder.

## 1. Set up the PC

1. Download `HomeStreamer.exe` from the [latest release](../../releases/latest) and put it in its own folder, for example `C:\HomeStreamer\`.
2. Double-click it once. It creates `config.json` next to itself, shares your **Videos** folder, and shows the address it's using.
3. When Windows Firewall asks, allow access on **private networks**. The TV can't connect without this.
4. To share other folders, close the window, open `config.json` in Notepad, and edit `folders`. In JSON, backslashes are written twice:

   ```json
   {
     "name": "Living room PC",
     "port": 8095,
     "folders": [
       "D:\\Movies",
       "D:\\TV Shows"
     ]
   }
   ```

   Then start `HomeStreamer.exe` again.
5. To check it's working, open the address it shows (like `http://192.168.1.20:8095/`) in a browser on the PC.

**Start automatically with Windows:** press `Win + R`, type `shell:startup`, press Enter, and put a shortcut to `HomeStreamer.exe` in the folder that opens. In the shortcut's properties, set **Run** to **Minimized**.

Also make sure the PC doesn't go to sleep while you watch (Settings → System → Power).

## 2. Install the app (TV, phone or tablet)

The app isn't on the Play Store, so you install the APK file yourself:

**Phone or tablet:** open the [latest release](../../releases/latest) in the browser, tap `HomeStreamer-Android.apk`, and allow your browser to install apps when asked.

**TV:**

1. On the TV, install **Downloader** (by AFTVnews) from the Play Store.
2. Allow it to install apps: Settings → Apps → Security & restrictions → **Unknown sources** → turn on Downloader. The exact menu names vary by TV.
3. In Downloader, enter the link to `HomeStreamer-Android.apk` from the [latest release](../../releases/latest) and install it.

Or with a computer on the same network: enable Developer options and network debugging on the TV, then run `adb connect <tv-ip>` and `adb install HomeStreamer-Android.apk`.

## 3. Watch

Open **Home Streamer**. It finds the PC automatically. If it doesn't, type the PC's address (shown in the server window).

On a phone or tablet, tap the video to show the controls. TV remote controls in the player:

| Button | Action |
| --- | --- |
| OK | Show the controls (subtitles button, audio and speed in settings) |
| Left / Right (controls hidden) | Back 10 seconds / forward 30 seconds |
| Back | Hide the controls, or leave the video |

## 4. Watch away from home (Tailscale)

[Tailscale](https://tailscale.com) links your PC and your devices into a private, encrypted network. Nothing is opened on your router, and only devices signed in to your Tailscale account can reach the server.

1. Create a free Tailscale account, then install Tailscale on the **Windows PC** and sign in.
2. Install **Tailscale** from the Play Store on your **phone or tablet** (or TV) and sign in with the same account. Keep it switched on when you're away.
3. Find the PC's Tailscale address. The Home Streamer server window shows it as "Tailscale (away from home) address", for example `100.101.102.103:8095`.
4. In the Home Streamer app, open **Settings → Away from home**, type that address, and press **Save**.

The app tries the home address first and switches to Tailscale automatically when you're out. The header shows "Away (Tailscale)" when it's using it.

If the app can't connect over Tailscale, Windows may treat the Tailscale network as public. Open **Windows Security → Firewall & network protection → Allow an app through firewall**, find **HomeStreamer**, and tick **Public** too.

Videos are sent as the original file. Away from home, smooth playback depends on your home internet's upload speed: typical 1080p files are fine, but very high-bitrate 4K files may stutter.

## Building from source

- Server: `cd server && go test ./... && GOOS=windows GOARCH=amd64 go build -o HomeStreamer.exe .`
- Android app: open `android-tv/` in Android Studio, or run `./gradlew assembleRelease`.
- GitHub Actions builds both on every push and pull request. Each push to `main` publishes a new release with `HomeStreamer.exe` and `HomeStreamer-Android.apk`.

### Signing the APK

Releases are signed with a permanent key stored in the repository's Actions secrets, so new versions install over the old one. The secrets are `SIGNING_KEYSTORE_B64` (the `.jks` file in base64), `SIGNING_STORE_PASSWORD`, `SIGNING_KEY_ALIAS` and `SIGNING_KEY_PASSWORD`. Keep a private backup of the key: if it's lost, users must uninstall before installing a build signed with a new one. Without the secrets, CI falls back to a throwaway debug key.

## Server API

| Endpoint | Returns |
| --- | --- |
| `GET /api/info` | Server name and version |
| `GET /api/browse?id=<folder id>` | Folder contents. An empty id is the library root |
| `GET /api/video/<id>` | One video with its subtitle files |
| `GET /stream/<id>` | The video file, with HTTP range support |
| `GET /subs/<id>` | A subtitle file as UTF-8 SRT (`?format=vtt` for WebVTT) |
| UDP `47820` | Answers `HOMESTREAMER_DISCOVER` broadcasts so the app finds the PC |

The server only serves files inside the shared folders and is meant for your home network. Don't forward its port to the internet.
