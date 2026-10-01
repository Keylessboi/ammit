
import json

recs = json.load(open('records.json'))
shown = False
for r in recs:
    f = r.get('fields', {})
    if 'chosen' in f and 'rejected' in f:
        print('  prompt:  ', f.get('prompt', '')[:62])
        print('  chosen:  ', f['chosen'][:62])
        print('  rejected:', f['rejected'][:62])
        shown = True
        break
if not shown:
    print('  (this page carried prose only; no preference records)')
