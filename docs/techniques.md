# Permutation techniques

Ten techniques run by default. Each models a specific way a real visitor ends up somewhere other than
the domain they meant, which is also why they are worth running together: a squatter picks the
mechanism, not the tool.

| Technique | What it does | `example.com` becomes |
| --- | --- | --- |
| `omission` | drops one character | `exmple.com` |
| `transposition` | swaps two adjacent characters | `exapmle.com` |
| `keyboard` | replaces a character with a physical neighbour on qwerty, qwertz or azerty | `ezample.com` |
| `addition` | appends one character — the trailing fat-finger, and the pluralised name | `examples.com` |
| `hyphenation` | inserts a hyphen — the word-split spelling of a compound name | `exam-ple.com` |
| `vowel-swap` | replaces one vowel with another — a mis-hit vowel, or a near-homophone | `exemple.com` |
| `bitsquatting` | flips a single bit in one character | `dxample.com` |
| `dot-insertion` | inserts a dot inside the label | `ex.ample.com` |
| `tld` | swaps the suffix for another one | `example.co` |
| `homoglyph` | substitutes a look-alike character or pair | `examp1e.com` |

Two of them are not typos at all, and are easy to misread in a report:

- **`bitsquatting`** models a *memory error*, not a keystroke. A bit flips in a cached hostname —
  through faulty RAM, or a cosmic ray ([yes, for real](https://www.youtube.com/watch?v=vj4RW39KICA)) — and a request meant for `example.com` leaves for
  `dxample.com` with nobody having typed anything. Registering the flip neighbours of a popular
  domain is a real, documented squatter pattern, which is why these candidates are worth resolving
  even though no human would ever type one.
- **`dot-insertion`** is the only technique that moves the **registrable boundary** rather than
  editing a label. The candidate `ex.ample.com` is not a variant of `example.com` at all: the name a
  squatter actually registers is `ample.com`, served with `ex` as a subdomain so the address bar
  reads almost right. Registration and wildcard checks therefore target `ample.com`, and that is the
  name a triage verdict is filed against.

`homoglyph` and `tld` are the two fan-out-heavy ones. `homoglyph` substitutes twice — once over the
seed's label and again over everything that produced — so a compounded look-alike (a digit
substitution *and* a script substitution) is reachable. `tld` sweeps whatever `--tld-profile` or
`--tld-file` selects, and is deliberately exempt from the per-technique cap: its size is exactly the
list you chose, so capping it would quietly turn `--tld-profile full` into "the IANA list up to
roughly the letter g".

```sh
yatt scan example.com --technique bitsquatting,dot-insertion
yatt scan example.com --technique tld --tld-profile full
```
