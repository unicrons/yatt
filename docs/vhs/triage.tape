# Demo: a triage verdict recorded once filters every future scan.
# Regenerate from the repo root after `go build -o yatt .`:
#   vhs docs/vhs/triage.tape
# The hidden setup points HOME at a throwaway directory so the demo database
# never touches the real one. The technique pair keeps the table narrow enough
# for a legible font.

Output docs/img/triage.gif

Set Shell "bash"
Set FontSize 16
Set Width 1720
Set Height 560
Set Padding 16
Set Framerate 20
Set TypingSpeed 40ms
Set WaitTimeout 180s

Hide
Type `export HOME="$(mktemp -d)"; alias yatt=./yatt; clear`
Enter
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
