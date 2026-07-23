# Demo: a triage verdict recorded once filters every future scan.
# Regenerate from the repo root after `go build -o yatt .`:
#   vhs docs/vhs/triage.tape
# The hidden setup points HOME and XDG_CONFIG_HOME at a throwaway directory so
# the demo database never touches the real one (os.UserConfigDir reads XDG on
# Linux and HOME on macOS). The technique pair keeps the table narrow enough
# for a legible font.

Output docs/img/triage.gif

Set Shell "bash"
Set FontSize 16
# 60px wider than scan.tape: the rescan's DIFF column holds "unchanged", not "new".
Set Width 1720
Set Height 560
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
Sleep 2s

Type "yatt triage example.com xample.com --status owned --note 'defensive registration'"
Sleep 500ms
Enter
Wait
Sleep 1500ms

Type "yatt scan example.com --technique omission,dot-insertion --exclude-status owned"
Sleep 500ms
Enter
Wait
Sleep 4s

Hide
Type `rm -rf "$HOME"`
Enter
Wait
