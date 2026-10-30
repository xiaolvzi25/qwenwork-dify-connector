import unittest,importlib.machinery,importlib.util,tempfile,os,json,threading,time,urllib.request,urllib.error,concurrent.futures
from unittest.mock import patch
from pathlib import Path
entry=Path(__file__).resolve().parents[1]/'package/bin/darwin_arm64/dify-qwen'
l=importlib.machinery.SourceFileLoader('adapter',str(entry));spec=importlib.util.spec_from_loader(l.name,l);m=importlib.util.module_from_spec(spec);l.exec_module(m)
class Tests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.env=patch.dict(os.environ,{'QWEN_DIFY_CONFIG_DIR':self.tmp.name,'DIFY_TOKEN':'SHOULD_NOT_PASS','DIFY_SERVER':'https://wrong.invalid'});self.env.start()
 def tearDown(self):self.env.stop();self.tmp.cleanup()
 def test_binary(self):m.verify_binary()
 def test_server_validation(self):
  self.assertEqual(m.normalize_server('https://dify.example/'),'https://dify.example')
  for s in ['http://dify.example','https://u:p@dify.example','https://dify.example/?token=x','https://dify.example/#f','file:///tmp','https://dify.example/\n']:
   with self.assertRaises(m.ConnectorError):m.normalize_server(s)
 def test_environment_isolation(self):
  e=m.environment();self.assertNotIn('DIFY_TOKEN',e);self.assertNotIn('DIFY_SERVER',e);self.assertTrue(e['DIFY_CONFIG_DIR'].startswith(self.tmp.name))
 def test_private_config(self):
  m.save_server('https://dify.example');self.assertEqual(m.saved_server(),'https://dify.example');self.assertEqual((Path(self.tmp.name)/'settings.json').stat().st_mode&0o777,0o600)
 def test_status_network_error_not_unauthorized(self):
  with patch.object(m,'account',side_effect=m.ConnectorError('E-0004','network')):
   with self.assertRaises(m.ConnectorError):m.status()
 def test_status_not_logged_in(self):
  with patch.object(m,'account',side_effect=m.ConnectorError('E-0001','login')):self.assertEqual(m.status(),{'status':'unauthorized'})
 def test_403_not_login(self):
  with self.assertRaises(m.ConnectorError) as ctx:m.require_success(4,{'error':{'http_status':403}})
  self.assertEqual(ctx.exception.code,'E-0005')
 def test_unknown_identity_not_authorized(self):
  with patch.object(m,'invoke',return_value=(0,{'unexpected':True})):
   with self.assertRaises(m.ConnectorError):m.account()
 def test_identity_redaction(self):
  with patch.object(m,'invoke',return_value=(0,{'id':'a','name':'tester','access_token':'SECRET'})):self.assertEqual(m.account(),{'id':'a','name':'tester'})
 def test_web_rejected(self):
  with patch.dict(os.environ,{'QWENWORK_SANDBOX_ID':'test'}):
   with self.assertRaises(m.ConnectorError):m.auth_portal()
 def test_unsafe_passthrough(self):
  for args in [['cli','get','config'],['cli','login'],['cli','get','workspace','--verbose']]:
   with self.assertRaises(m.ConnectorError):m.main(args)
 def test_real_portal_with_mock_oauth(self):
  urls=[];calls=[]
  def fake(args,timeout=35):
   calls.append(args)
   if '--no-wait' in args:return 0,{'verification_uri':'https://dify.example/activate','user_code':'TEST-CODE','expires_in':30,'interval':5}
   if '--resume' in args:return 0,{'email':'test@example.invalid'}
   return 0,{'id':'mock-user','name':'mock-test'}
  def request(url,data=None,headers=None):
   return urllib.request.build_opener(urllib.request.ProxyHandler({})).open(urllib.request.Request(url,data=data,headers=headers or {}),timeout=5)
  with patch.object(m,'invoke',side_effect=fake),patch.object(m.webbrowser,'open',side_effect=lambda u:urls.append(u) or True):
   with concurrent.futures.ThreadPoolExecutor() as pool:
    future=pool.submit(m.auth_portal,'https://dify.example',False,30)
    for _ in range(50):
     if urls:break
     time.sleep(.05)
    base=urls[0];page=request(base).read().decode();csrf=page.split("csrf='")[1].split("'")[0];origin=base.rsplit('/',1)[0]
    with self.assertRaises(urllib.error.HTTPError):request(base+'/start',json.dumps({'server':'https://dify.example'}).encode())
    request(base+'/start',json.dumps({'server':'https://dify.example'}).encode(),{'Origin':origin,'X-CSRF':csrf,'Content-Type':'application/json'})
    stages=[]
    for _ in range(160):
     s=json.load(request(base+'/state'));stages.append(s['stage'])
     if s['stage']=='authorized':break
     time.sleep(.05)
    self.assertIn('pending',stages);self.assertEqual(s['stage'],'authorized');self.assertEqual(s['identity']['id'],'mock-user')
    self.assertNotIn('access_token',json.dumps(s));self.assertEqual(future.result(timeout=15),0)
    self.assertTrue(any('--resume' in c for c in calls))
if __name__=='__main__':unittest.main(verbosity=2)
