from pathlib import Path
import re
root=Path(__file__).resolve().parents[1]
errors=[]
# Location ingestion is the only permitted mobile raw-fix path; no coordinate response DTOs.
for f in (root/'internal/api').glob('*.go'):
 if f.name.endswith('_test.go'): continue
 text=f.read_text()
 if f.name!='geo.go' and re.search(r'json:"(?:lat|lng|latitude|longitude)"',text): errors.append(str(f))
for f in (root/'apps/mobile/lib').rglob('*.dart'):
 text=f.read_text()
 if re.search(r'(?i)marketplace|nexus.?coins|seller.?dashboard|checkout',text):errors.append(str(f))
 if f.name!='api.dart' and re.search(r"['\"](?:lat|lng|latitude|longitude)['\"]",text):errors.append(str(f))
if errors: raise SystemExit('Guardrail violations: '+', '.join(errors))
print('Static guards passed: no commerce UI; raw-fix ingestion isolated. Dynamic tests remain required.')
