"""Shapes order-dependent at head and not at base on the multi-row grid, by class."""
import collections, json, sys

def load(p):
    d = collections.defaultdict(dict)
    for line in open(p, encoding='utf-8'):
        s, o, j = line.rstrip('\n').split('\t')
        if not o.startswith('ctl'):
            d[s][o] = j
    return d

def facts_only(orders):
    return orders

b, h = load(sys.argv[1]), load(sys.argv[2])
def od(x):
    return {s for s, o in x.items() if len(set(o.values())) > 1}
db, dh = od(b), od(h)
print('order-dep base', len(db), 'head', len(dh), 'head-only', len(dh - db), 'base-only', len(db - dh))
c = collections.Counter()
for s in dh - db:
    p = s.split('|')
    c[(p[1], p[2], p[-1])] += 1
for k, n in c.most_common(30):
    print(' ', k, n)
c2 = collections.Counter()
for s in dh - db:
    p = s.split('|')
    c2[(p[2] in ('own=""', 'own=" "'), p[-1], p[4])] += 1
for k, n in c2.most_common(30):
    print('  blank-own,anchor,other', k, n)
