# tv-update

The TV APK the hub offers paired TVs (`GET /api/tv/update`, `backend/tv_update.go`).
`scripts/Publish-TVUpdate.ps1` (run by `scripts/Build-TV.ps1`) writes
`camerahub-tv.apk` and `camerahub-tv.json` here; the Dockerfile bundles the
folder into the app image at `/tv-update`. Both files are gitignored. An empty
folder means the hub offers no update.
