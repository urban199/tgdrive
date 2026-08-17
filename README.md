# TgDrive

Cloud drive berbasis Telegram. APK langsung jalan tanpa server lain dan tanpa FTP.

- Android: Kotlin + Jetpack Compose.
- Go: MTProto runtime dibungkus menjadi AAR memakai `gomobile`.
- Storage: Telegram chat/channel milik sendiri.
- HTTP API Go hanya listen di `127.0.0.1` di dalam proses APK.
- Build: GitHub Actions.

## Arsitektur standalone

```text
APK Kotlin
  └── tgdrive.aar
        └── Go MTProto + HTTP API loopback
              └── Telegram storage chat/channel
```

Saat aplikasi dibuka, Kotlin memanggil Go bridge. Go login ke Telegram, membuka API di loopback, lalu Kotlin memakai API tersebut untuk menampilkan drive. Tidak ada VPS, binary backend terpisah, URL server, atau FTP yang diperlukan.

Session MTProto dan cache transfer disimpan di private app storage Android. Index drive aktif hanya berada di memory Go dan selalu dimuat dari backup terenkripsi di Telegram saat runtime mulai atau endpoint membaca drive dipanggil. Tidak ada index lokal yang menjadi sumber kebenaran.

## Setup pertama

Isi kredensial Telegram di Settings APK:

- Bot token dari BotFather.
- Telegram API ID dan API hash dari <https://my.telegram.org>.
- Chat/channel storage. Tambahkan bot sebagai anggota/admin sesuai kebutuhan.
- Encryption key. Gunakan string rahasia panjang.

Kredensial disimpan di `SharedPreferences` private aplikasi dan tidak dimasukkan ke source code atau GitHub Actions.

## Fitur

- Browse folder langsung dari Telegram drive.
- Upload file.
- Download file ke folder Downloads Android.
- Stream video dengan HTTP Range dan kontrol Media3.
- Thumbnail video otomatis dari frame pertama.
- Buka audio dengan player internal.
- Buka PDF di viewer internal.
- Buka gambar dengan zoom dan pan.
- Grid atau list view untuk drive.
- Buka gambar dan thumbnail.
- Buat folder.
- Rename file/folder.
- Delete file/folder.
- Search file.
- Sinkronisasi foto saja, video saja, atau keduanya.
- Sinkronisasi otomatis tiap 12 jam saat ada internet.
- Enkripsi file dan nama file sebelum dikirim ke Telegram.
- Backup index terenkripsi di Telegram; state index aktif hanya di memory.
- Cache download lokal hanya untuk mempercepat transfer, bukan sumber metadata.

## Build APK

Build utama lewat GitHub Actions karena build Go AAR dan Android SDK cukup berat untuk Termux.

Workflow menjalankan:

1. `go test ./...`.
2. Install Android SDK dan NDK.
3. Generate `tgdrive.aar` dengan `gomobile bind` untuk semua ABI Android.
4. Build release signed per ABI serta APK universal.
5. Upload setiap APK release sebagai artifact file tunggal tanpa archive.

Artifact release tersedia sebagai file terpisah:

- `app-armeabi-v7a-release.apk`
- `app-arm64-v8a-release.apk`
- `app-x86-release.apk`
- `app-x86_64-release.apk`
- `app-universal-release.apk`

Workflow memakai `actions/upload-artifact@v7` dengan `archive: false`, sehingga APK tidak digabung ke satu ZIP artifact. Release APK ditandatangani memakai GitHub Secrets; private key tidak disimpan di repository.

Secrets yang diperlukan untuk signed release:

- `TGDRIVE_KEYSTORE_BASE64`: file keystore dalam Base64.
- `TGDRIVE_KEYSTORE_PASSWORD`.
- `TGDRIVE_KEY_ALIAS`.
- `TGDRIVE_KEY_PASSWORD`.

Release artifact memakai nama `app-<abi>-release.apk`. Jika signing secret belum tersedia, workflow sengaja gagal sebelum build agar tidak menghasilkan APK release yang tidak ditandatangani.

Run workflow manual:

```sh
gh workflow run build.yml -R bexcodex/tgdrive-apk
```

Download artifact:

```sh
gh run download <RUN_ID> \
  -R bexcodex/tgdrive-apk \
  -n app-arm64-v8a-release.apk
```

## Build lokal

Memerlukan Java 17, Android SDK, Android NDK, dan Go.

```sh
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20260709172247-6129f5bee9d5
gomobile init
mkdir -p android/app/libs
gomobile bind \
  -target=android \
  -androidapi=26 \
  -javapkg=com.tgdrive \
  -o android/app/libs/tgdrive.aar \
  ./backend/mobile/bridge
gradle -p android assembleRelease
```

## Catatan sinkronisasi

- Foto masuk ke `<folder-sync>/Foto`.
- Video masuk ke `<folder-sync>/Video`.
- Nama remote diberi prefix MediaStore ID supaya file bernama sama tidak saling menimpa.
- Android menyimpan signature lokal; index Telegram juga menyimpan SHA-256 untuk memverifikasi file tanpa upload ulang.
- Ubah folder sync atau hapus data aplikasi jika ingin memulai ulang sinkronisasi.
