# Demo: yatt scan — the live progress line, then the report.
# Regenerate from the repo root after `go build -o yatt .`:
#   vhs docs/vhs/scan.tape
# The hidden setup points HOME and XDG_CONFIG_HOME at a throwaway directory so
# the demo database never touches the real one (os.UserConfigDir reads XDG on
# Linux and HOME on macOS). The technique pair keeps the table narrow enough
# for a legible font.

Output docs/img/scan.gif

Set Shell "bash"
Set FontSize 16
Set Width 1660
Set Height 420
Set Padding 16
Set Framerate 20
Set TypingSpeed 40ms
Set WaitTimeout 180s

Hide
Type `export HOME="$(mktemp -d)"; export XDG_CONFIG_HOME="$HOME/.config"; alias yatt=./yatt; clear`
Enter
Wait
Show

Type "yatt scan example.com --technique omission,dot-insertion"
Sleep 500ms
Enter
Wait
Sleep 4s

Hide
Type `rm -rf "$HOME"`
Enter
Wait
