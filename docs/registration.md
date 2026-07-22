# How "registered" is decided

Registration is read from the **response code of an NS query at the candidate's registrable domain
(eTLD+1)** — NXDOMAIN means unregistered, NOERROR means registered.

The naive alternative, treating an A-record NXDOMAIN as "unregistered", produces false negatives on
MX-only and delegation-only domains. Those are precisely the parked and defensive registrations a
typosquatting scan exists to surface, so A, AAAA, MX and NS presence are reported as separate signals
rather than as evidence of registration.

The seed is split with a public-suffix list rather than on the last dot, for the same reason: a naive
split of `example.co.uk` would query `co.uk`, which always answers NOERROR and would mark every
candidate registered.

## Zones that never say NXDOMAIN

Some registries — `.gov`, `.ph` and `.fm` among them — answer NOERROR for names that do not exist.
Taken at face value the rcode test reports *every* candidate under them as registered, with zero DNS
records to show for it.

The wildcard probes already answer this: the first random-label lookup reveals whether the zone lets
a nonexistent name be nonexistent. When it does not, only the candidates with **no records at all**
are flipped back to unregistered — a genuinely registered name in such a zone still shows a real
delegation or real records, so it survives. The reported `rcode` is left as the zone gave it: the
zone did say NOERROR, it just does not mean registration there.

This is separate from catch-all detection. A zone can suppress NXDOMAIN without handing out
addresses, which is exactly what these registries do, so neither check subsumes the other.
