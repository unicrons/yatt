# Scan profiles and the config file

`--profile` is a preset for the knobs that shape a scan: `techniques`, `tld_profile`, `concurrency`,
`qps`, `timeout` and `limit`. Two are built in:

- `quick` — omission, transposition and keyboard adjacency against the common TLD list, at modest
  concurrency, for a fast first look.
- `full` — every technique, including the two fan-out-heavy ones (homoglyph, TLD swap against the
  whole IANA list), at higher concurrency.

```sh
yatt scan example.com --profile quick
yatt scan example.com --profile full
```

A profile is a preset, not a lock: any flag it sets can still be overridden individually, and the flag
always wins.

```sh
yatt scan example.com --profile quick --technique homoglyph   # quick's techniques, but this one instead
yatt scan example.com --profile quick --tld-profile full      # quick's techniques, full's TLD list
```

`--config` points at a YAML/JSON/TOML file that can redefine a builtin profile or add new ones under
`profiles`, naming only the fields it wants to change — anything it leaves out still comes from the
builtin of the same name, or stays unset for a name with no builtin:

```yaml
# yatt.yaml
profile: quick        # selected when --profile is not given

profiles:
  quick:
    tld_profile: full  # quick, but sweeping the full TLD list instead of the curated one

  thorough:             # a profile with no builtin counterpart
    techniques: [omission, transposition, keyboard, tld]
    tld_profile: full
    concurrency: 30
    timeout: 5s          # a duration string; a bare number is rejected as ambiguous
    limit: 300
```

```sh
yatt --config yatt.yaml scan example.com --profile thorough
```

Precedence, low to high: the builtin profile < a same-named profile in the config file < the
`YATT_PROFILE` environment variable < an explicit `--profile` < a flag the command line actually set
for that one knob. A profile field left unset at every level does not blank a flag's own default —
scanning with no `--profile` at all behaves exactly as if profiles did not exist.
