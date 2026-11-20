# Backup and restore

## What it does

STKR keeps all data in one SQLite file, `stkr.db`, in the data folder. Backups are complete, consistent copies of that file in the `backups` folder.

| Backup | When | Kept |
|---|---|---|
| `stkr-<UTC time>.db`, for example `stkr-20261120T183000Z.db` | `stkr backup`; also once a day by the background server `stkr serve` (coming in a later release) | The newest 14. Older ones are deleted automatically. |
| `pre-migrate-<version>-<UTC time>.db` | Automatically, before a new STKR version changes the database layout. `<version>` is the layout version the copy contains. | Never deleted automatically. |
| `legacy-<UTC time>/` | `stkr import-legacy`. See [Legacy import](legacy-import.md). | Never deleted automatically. |

A backup is safe to take while STKR is in use.

## How to use

Make a backup now. The command prints the file path:

```
stkr backup
stkr backup --json
```

Restore a backup (manual on purpose):

1. Stop every running STKR process (`stkr serve`, `stkr mcp`, open terminals running `stkr`).
2. In the data folder, move `stkr.db` aside, and delete `stkr.db-wal` and `stkr.db-shm` if they exist.
3. Copy the backup you want to `stkr.db`.
4. Start STKR again and check with `stkr day` or `stkr week`.

On Windows (PowerShell), with the default data folder:

```
cd $HOME\.stkr
Rename-Item stkr.db stkr.db.old
Remove-Item stkr.db-wal, stkr.db-shm -ErrorAction SilentlyContinue
Copy-Item backups\stkr-20261120T183000Z.db stkr.db
```

On Linux or macOS:

```
cd ~/.stkr
mv stkr.db stkr.db.old && rm -f stkr.db-wal stkr.db-shm
cp backups/stkr-20261120T183000Z.db stkr.db
```

## Configuration keys

None. The backup folder is always `backups` inside the data folder.

## Troubleshooting

| Problem | What to do |
|---|---|
| `backup file already exists` | Two backups were requested at the same moment. Run the command again. |
| No daily backup appears | Daily backups are made by the background server. Use `stkr backup` when it is not running. |
| Disk filling up | Only the newest 14 regular backups are kept. Delete old `pre-migrate-*` files and `legacy-*` folders by hand if you do not need them. |
| `database is locked` after a restore | A STKR process was still running. Stop it and repeat the restore. |
