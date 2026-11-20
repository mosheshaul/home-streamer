# Home Streamer

Watch the videos on your Windows PC on an Android TV, over your home network.

- **`server/`**: a small Windows program (`HomeStreamer.exe`, written in Go) that shares your video folders.
- **`android-tv/`**: the TV app (Kotlin, Compose for TV, Media3/ExoPlayer).

## What it does

- Browse your folders with the TV remote, with "Continue watching" and resume.
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

## 2. Install the app on the TV

The app isn't on the Play Store, so you install the APK file yourself:

1. On the TV, install **Downloader** (by AFTVnews) from the Play Store.
2. Allow it to install apps: Settings → Apps → Security & restrictions → **Unknown sources** → turn on Downloader. The exact menu names vary by TV.
3. In Downloader, enter the link to `HomeStreamerTV.apk` from the [latest release](../../releases/latest) and install it.

Or with a computer on the same network: enable Developer options and network debugging on the TV, then run `adb connect <tv-ip>` and `adb install HomeStreamerTV.apk`.

## 3. Watch

Open **Home Streamer** on the TV. It finds the PC automatically. If it doesn't, type the PC's address (shown in the server window).

Remote controls in the player:

| Button | Action |
| --- | --- |
| OK | Show the controls (subtitles button, audio and speed in settings) |
| Left / Right (controls hidden) | Back 10 seconds / forward 30 seconds |
| Back | Hide the controls, or leave the video |

## Building from source

- Server: `cd server && go test ./... && GOOS=windows GOARCH=amd64 go build -o HomeStreamer.exe .`
- TV app: open `android-tv/` in Android Studio, or run `./gradlew assembleRelease`.
- GitHub Actions builds both on every push and pull request. Each push to `main` publishes a new release with `HomeStreamer.exe` and `HomeStreamerTV.apk`.

### Signing the APK

Without a signing key, CI signs with a throwaway debug key, so each new build has to be installed fresh (uninstall first). To keep updates installable over the old version, create a key once and add it as repository secrets:

```sh
keytool -genkeypair -v -keystore release.jks -alias homestreamer -keyalg RSA -keysize 2048 -validity 10000
base64 -w0 release.jks   # paste into the secret SIGNING_KEYSTORE_B64
```

Secrets: `SIGNING_KEYSTORE_B64`, `SIGNING_STORE_PASSWORD`, `SIGNING_KEY_ALIAS` (`homestreamer`), `SIGNING_KEY_PASSWORD`.

## Server API

| Endpoint | Returns |
| --- | --- |
| `GET /api/info` | Server name and version |
| `GET /api/browse?id=<folder id>` | Folder contents. An empty id is the library root |
| `GET /api/video/<id>` | One video with its subtitle files |
| `GET /stream/<id>` | The video file, with HTTP range support |
| `GET /subs/<id>` | A subtitle file as UTF-8 SRT (`?format=vtt` for WebVTT) |
| UDP `47820` | Answers `HOMESTREAMER_DISCOVER` broadcasts so the TV finds the PC |

The server only serves files inside the shared folders and is meant for your home network. Don't forward its port to the internet.
