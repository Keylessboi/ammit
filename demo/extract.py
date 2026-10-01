
import json, re, html

src = open('page.html').read()
blocks = re.findall(r'<script type="application/ld\+json">(.*?)</script>', src, re.S)
recs = [json.loads(html.unescape(b)) for b in blocks]
print(f'{len(recs)} training records on the page')
json.dump(recs, open('records.json', 'w'), indent=2)
