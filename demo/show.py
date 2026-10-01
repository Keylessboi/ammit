
import json
import textwrap

recs = json.load(open('records.json'))

def emit(key, value, indent='  '):
    text = ' '.join(value.split())
    lines = textwrap.wrap(text, width=66) or ['']
    print(f'{indent}{key:9s} {lines[0]}')
    for line in lines[1:]:
        print(f'{indent}{" ":9s} {line}')

for r in recs:
    f = r.get('fields', {})
    if 'chosen' in f and 'rejected' in f:
        print('  one record from the page:')
        emit('prompt', f['prompt'])
        emit('chosen', f['chosen'])
        emit('rejected', f['rejected'])
        break
