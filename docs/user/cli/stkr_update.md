## stkr update

Check for a new release and update this program

### Synopsis

Update stkr from GitHub Releases. The download is verified against the release's checksums.txt (SHA-256); on a mismatch nothing is changed.

--check only reports: exit code 0 means up to date, 10 means an update is available.
Without --check the new version is installed after you confirm (use --yes in scripts). A newer release is never installed over a development build without --force, and older versions only with an explicit --version.

```
stkr update [flags]
```

### Examples

```
  stkr update --check
  stkr update
  stkr update --yes
  stkr update --version v0.1.0
```

### Options

```
      --check            only check; exit 10 if an update is available
      --force            allow replacing a development build
  -h, --help             help for update
      --version string   install this release (also allows going back to an older one)
  -y, --yes              install without asking
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

