"""On one capture, check the versioned rows of direct blank groups against rule 1.1.

For every call with a blank own fact, no anchor, and an other fact that alone yields
nothing on the host: each finding naming a versioned row must equal the own-only
control's finding for that row (status, reason, fix, advisory, evidence). Also counts
order-dependent shapes (per fact order and per row order)."""
import collections, json, sys

d = collections.defaultdict(dict)
for line in open(sys.argv[1], encoding='utf-8'):
    s, o, j = line.rstrip('\n').split('\t')
    d[s][o] = [json.loads(x) for x in (json.loads(j) or [])]

def rowid(f):
    return sorted(x for x in f['EvidenceFactIDs'] if x.startswith('row-'))

def view(f):
    return (f['Status'], f['MatchReason'], f.get('FixedVersion'), f.get('AdvisoryID'), tuple(sorted(f['EvidenceFactIDs'])))

c = collections.Counter(); ex = []; kf = 0; by_topo = collections.Counter()
for s, orders in d.items():
    p = s.split('|')
    if not (p[2] in ('own=""', 'own=" "')) or p[-1] != 'none':
        continue
    if p[4] not in ('other=osv-spelled-nopurl', 'other=nvd-os-nopurl', 'other=blank-source-blank-eco', 'other=nvd-blank-purl'):
        continue
    for o, fs in orders.items():
        if o.startswith('ctl'):
            continue
        ctl_all = {r: f for ro in ('ctl.r0', 'ctl.r1') for f in orders[ro] for r in rowid(f)}
        for f in fs:
            for r in rowid(f):
                if r == 'row-n' or r == 'row-m':
                    continue
                c['versioned-row findings'] += 1
                want = ctl_all.get(r)
                if want is None:
                    c['no control row'] += 1
                    continue
                if view(f) == view(want):
                    c['equals control'] += 1
                else:
                    c['differs from control'] += 1
                    by_topo[p[1]] += 1
                    if f['Status'] == 'not_affected_known_fixed' and want['Status'] != 'not_affected_known_fixed':
                        kf += 1
                    if len(ex) < 4:
                        ex.append((s, o, view(f), view(want)))
print(dict(c)); print('false known-fixed on a versioned row', kf); print('differs by topology', by_topo.most_common())
for e in ex:
    print('DIFF', e)
od = sum(1 for s, o in d.items() if len({json.dumps(o[k]) for k in o if not k.startswith('ctl')}) > 1)
print('shapes', len(d), 'order-dependent (fact or row order)', od)
