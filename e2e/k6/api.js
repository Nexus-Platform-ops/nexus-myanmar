// Staging-only load harness. Supply a scoped account token, never a real user's token.
import http from 'k6/http';
import { check, sleep } from 'k6';
export const options = { vus: Number(__ENV.VUS || 10), duration: '30s', thresholds: { http_req_failed: ['rate<0.01'], 'http_req_duration{endpoint:feed}': ['p(95)<120'] } };
export default function () {
 const base=__ENV.API_BASE;
 if(!base || !__ENV.TOKEN) throw new Error('API_BASE and TOKEN required');
 const r=http.get(`${base}/v1/feed`,{headers:{Authorization:`Bearer ${__ENV.TOKEN}`},tags:{endpoint:'feed'}});
 check(r,{'feed successful':r=>r.status===200}); sleep(1);
}
