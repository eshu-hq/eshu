"""Adds the #9990 parity capture calls to one scratch worktree."""

import sys
root=sys.argv[1]
p=root+'/go/internal/reducer/supplychain/core/impact.go'
s=open(p).read()
old='''	findings, _, _ := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	return findings
'''
assert s.count(old)==1
s=s.replace(old,'''	findings, _, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	parityCapture("fleet", "", envelopes, findings, err)
	return findings
''')
open(p,'w').write(s)
p=root+'/go/internal/reducer/supplychain/core/derived_partition.go'
s=open(p).read()
i=s.index('	findings, _, err := buildAuthorizedScanImpactFindingsWithQuarantine(legacyFindings, targets')
j=s.index('	return findings, err\n',i)
s=s[:j]+'	parityCapture("authorized", "", []any{legacyFindings, targets}, findings, err)\n'+s[j:]
open(p,'w').write(s)
