#!/usr/bin/env python3
"""Summarizes the b2a WAL harness output: mean WAL bytes per pass and variant, with the
change against the B1-shipped baseline, and the replay HOT share.

Usage: python3 wal_summary.py b2a-wal.txt
"""
import collections
import re
import statistics
import sys

rows = collections.defaultdict(list)
hot = collections.defaultdict(lambda: [0, 0])
sizes = collections.defaultdict(list)
for line in open(sys.argv[1]):
    m = re.match(r'RESULT shape="([^"]+)" variant="([^"]+)" mode=(\w+) rep=(\d+) pass=(\w+) records=(\d+) fpi=(\d+) bytes=(\d+) \| supports ins=(\d+) upd=(\d+) hot_upd=(\d+) del=(\d+)', line)
    if m:
        shape, var, mode, rep, ps, rec, fpi, by, ins, upd, hotu, dele = m.groups()
        rows[(shape, mode, ps, var)].append((int(rec), int(fpi), int(by), int(by) - 8192 * int(fpi)))
        if ps == 'replay':
            hot[(shape, var, mode)][0] += int(upd)
            hot[(shape, var, mode)][1] += int(hotu)
        continue
    m = re.match(r'SIZE shape="([^"]+)" variant="([^"]+)" mode=(\w+) rep=(\d+) index=(\S+) bytes=(\d+)', line)
    if m:
        sizes[(m.group(1), m.group(2), m.group(5))].append(int(m.group(6)))

order = ['B1-shipped', '+plain', '+ordered', '+trim', '+image', '+plain+image', '+ordered+image']
for shape in sorted({k[0] for k in rows}):
    for mode in ('cold', 'warm'):
        print('== %s / %s' % (shape, mode))
        for ps in ('insert', 'replay', 'retract'):
            base = None
            for var in order:
                v = rows.get((shape, mode, ps, var))
                if not v:
                    continue
                by = statistics.mean(x[2] for x in v)
                nf = statistics.mean(x[3] for x in v)
                if var == 'B1-shipped':
                    base, base_nf = by, nf
                pct = '' if base is None or var == 'B1-shipped' else '(%+.1f%% vs B1; %+.1f%% without FPI bytes)' % (100 * (by - base) / base, 100 * (nf - base_nf) / base_nf)
                print('  %-8s %-15s n=%d records=%9.0f fpi=%6.0f bytes=%11.0f %s' % (
                    ps, var, len(v), statistics.mean(x[0] for x in v), statistics.mean(x[1] for x in v), by, pct))
        for var in order:
            h = hot.get((shape, var, mode))
            if h and h[0]:
                print('  replay HOT share %-15s upd=%d hot=%d (%.1f%%)' % (var, h[0], h[1], 100 * h[1] / h[0]))
print('(without FPI bytes = bytes minus 8,192 per full-page image, which removes checkpoint-timing noise)')
print('== index sizes after 3,000 inserts and 3,000 replays (bytes, mean)')
for (shape, var, ix), v in sorted(sizes.items()):
    print('  %-34s %-15s %-34s %10.0f' % (shape, var, ix, statistics.mean(v)))
