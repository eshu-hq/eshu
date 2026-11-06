"""Compare two multi-row grid captures: base rows must survive byte for byte; report new rows."""
import collections, json, sys

def load(p):
    d = {}
    for line in open(p, encoding='utf-8'):
        s, o, j = line.rstrip('\n').split('\t')
        d[(s, o)] = json.loads(j)
    return d

b, h = load(sys.argv[1]), load(sys.argv[2])
print('calls', len(b), len(h), 'keys-equal', set(b) == set(h))
c = collections.Counter(); ex = []; topo_new = collections.Counter(); topo_lost = collections.Counter()
for k, hv in h.items():
    if k[1].startswith('ctl'):
        c['ctl-same' if b[k] == hv else 'ctl-diff'] += 1
        continue
    bs, hs = collections.Counter(b[k]), collections.Counter(hv)
    lost = bs - hs; added = hs - bs
    if not lost and not added:
        c['identical'] += 1
    elif lost:
        c['base-row-moved'] += 1
        topo_lost[k[0].split('|')[1]] += 1
        if len(ex) < 8:
            ex.append((k, list(lost)[:1], list(added)[:1]))
    else:
        c['only-added'] += 1
        topo_new[k[0].split('|')[1]] += 1
print(dict(c))
print('topology of only-added', topo_new.most_common())
print('topology of base-row-moved', topo_lost.most_common())
for k, l, a in ex:
    fl = json.loads(l[0]) if l else {}
    fa = json.loads(a[0]) if a else {}
    print('MOVED', k, '\n  was', fl.get('Status'), fl.get('MatchReason'), fl.get('FixedVersion'), fl.get('AdvisoryID'), fl.get('EvidenceFactIDs'),
          '\n  now', fa.get('Status'), fa.get('MatchReason'), fa.get('FixedVersion'), fa.get('AdvisoryID'), fa.get('EvidenceFactIDs'))
