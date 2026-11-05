"""Local smoke against running API + BFF; synthetic phone only, never prints tokens."""
import json, urllib.request, urllib.error
API='http://127.0.0.1:8080'
BFF='http://127.0.0.1:8081'
def call(method,path,body=None,token=None,base=API):
 data=json.dumps(body).encode() if body is not None else None
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 r=urllib.request.Request(base+path,data=data,headers=headers,method=method)
 with urllib.request.urlopen(r,timeout=10) as response:
  raw=response.read()
  return response.status,json.loads(raw) if raw else {}
assert call('GET','/health/ready')[0]==200
_,ch=call('POST','/v1/auth/otp/request',{'phone':'+959000000001'})
assert 'development_code' in ch, 'Run only against local development API'
_,session=call('POST','/v1/auth/otp/verify',{'challenge_id':ch['challenge_id'],'code':ch['development_code'],'dob':'1995-01-01','device':'Synthetic local smoke'})
token=session['access_token']
assert call('PUT','/v1/profile',{'real_name':'Synthetic Smoke','township':'kamayut','bio':'Local smoke only'},token)[0]==200
status,data=call('POST','/graphql',{'query':'{ feed { items { id body } } }'},token,BFF)
assert status==200 and 'errors' not in data
assert call('DELETE','/v1/account',{'confirm':'DELETE MY ACCOUNT'},token)[0]==204
print(json.dumps({'api_readiness':'PASS','development_otp_login':'PASS','social_profile_write':'PASS','authenticated_graphql_to_rest':'PASS','delete_synthetic_account':'PASS','credentials_logged':False},indent=2))
