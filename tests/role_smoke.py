"""Test role routing through an isolated CPA and local mock upstreams.
Usage: role_smoke.py <cpa-archive> <checksums> <plugin-zip>
No production config, credentials, ports or upstream services are used.
"""
import json
import hashlib
import os
import sys
import tempfile
import threading
import time
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import subprocess
from load_smoke import verify_checksum, extract_cpa, install_plugin, free_port, stop

class Mock(BaseHTTPRequestHandler):
    lock = threading.Lock()
    calls = []
    failures = {"a": 429, "b": 503}
    partial = False
    def log_message(self, *_): pass
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        account = self.path.split('/')[1]
        with self.lock:
            self.calls.append((account, body))
            failure = self.failures.get(account, 0)
        if failure:
            raw = json.dumps({"error":{"message":"mock unavailable", "type":"server_error"}}).encode()
            self.send_response(failure); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(raw))); self.end_headers(); self.wfile.write(raw); return
        model = body['model']
        if account == 'claude':
            raw=json.dumps({'id':'msg_mock','type':'message','role':'assistant','model':model,'content':[{'type':'text','text':'claude'}],'stop_reason':'end_turn','usage':{'input_tokens':10,'output_tokens':1}}).encode()
            self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw);return
        if body.get('stream'):
            chunks = [
                {"id":"chatcmpl-mock","object":"chat.completion.chunk","model":model,"choices":[{"index":0,"delta":{"role":"assistant","content":account},"finish_reason":None}]},
                {"id":"chatcmpl-mock","object":"chat.completion.chunk","model":model,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}},
            ]
            raw = ''.join('data: '+json.dumps(c)+'\n\n' for c in chunks).encode()+b'data: [DONE]\n\n'
            if self.partial and account == 'a':
                raw=('data: '+json.dumps(chunks[0])+'\n\n').encode()
                self.send_response(200);self.send_header('Content-Type','text/event-stream');self.send_header('Content-Length',str(len(raw)+100));self.end_headers();self.wfile.write(raw);self.wfile.flush();self.close_connection=True;return
            self.send_response(200); self.send_header('Content-Type','text/event-stream'); self.send_header('Content-Length',str(len(raw))); self.end_headers(); self.wfile.write(raw)
        else:
            raw = json.dumps({"id":"chatcmpl-mock","object":"chat.completion","model":model,"choices":[{"index":0,"message":{"role":"assistant","content":account},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}).encode()
            self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(raw))); self.end_headers(); self.wfile.write(raw)

opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
def http(port, path, body=None, management=False, method=None, extra_headers=None):
    raw = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(f'http://127.0.0.1:{port}{path}', data=raw, method=method, headers={'Authorization':'Bearer '+('role-smoke-admin' if management else 'role-smoke-client'), 'Content-Type':'application/json',**(extra_headers or {})})
    try:
        with opener.open(req, timeout=20) as r: return r.read()
    except urllib.error.HTTPError as e:
        raise AssertionError(f"{path}: HTTP {e.code}: {e.read().decode()}") from e

def main():
    if len(sys.argv)!=4: raise SystemExit(__doc__)
    archive,checksums,plugin = map(Path,sys.argv[1:]); verify_checksum(archive, checksums)
    mock = ThreadingHTTPServer(('127.0.0.1',0),Mock)
    threading.Thread(target=mock.serve_forever,daemon=True).start()
    with tempfile.TemporaryDirectory(prefix='role-smoke-') as tmp:
        root=Path(tmp);binary=extract_cpa(archive,root);install_plugin(plugin,root/'plugins',archive);(root/'auth').mkdir()
        port=free_port(); upstream=mock.server_port
        config={
            'host':'127.0.0.1','port':port,'auth-dir':str(root/'auth'), 'api-keys':['role-smoke-client'],
            'remote-management':{'secret-key':'role-smoke-admin','disable-control-panel':True},
            'request-retry':0,'disable-cooling':True,
            'claude-api-key':[{'api-key':'synthetic-claude','base-url':f'http://127.0.0.1:{upstream}/claude','models':[{'name':'claude-role-test','alias':'claude-role-test','thinking':{'levels':['low','medium','high','xhigh','max']}}]}],
            'openai-compatibility':[
                {'name':'role-'+a,'priority':priority,'base-url':f'http://127.0.0.1:{upstream}/{a}/v1','disable-cooling':True,'request-retry':0,'api-key-entries':[{'api-key':'synthetic-'+a}], 'models':[{'name':model,'alias':model,'thinking':{'levels':['low','medium','high','xhigh','max']}}]}
                for a,model,priority in [('a','role-primary',20),('b','role-primary',10),('g','role-secondary',100)]
            ],
            'plugins':{'enabled':True,'dir':str(root/'plugins'),'configs':{'kpatdev-quota-router':{'enabled':True,'mode':'active','state_dir':str(root/'state'),'client_routing':{'enabled':True,'desktop_caller_scopes':[hashlib.sha256(b'cli-proxy-api:caller-scope:v1\x00role-smoke-client').hexdigest()]},'roles':[
                {'name':'designer','enabled':True,'cooldown_seconds':1,'targets':[{'model':'role-primary','effort':'high'},{'model':'role-secondary','effort':'xhigh'}]},
                {'name':'claude-worker','enabled':True,'targets':[{'model':'claude-role-test','effort':'high'}]},
                {'name':'interactive','enabled':True,'allow_client_effort_override':True,'targets':[{'model':'role-secondary','effort':'xhigh'}]},
            ]}}}
        }
        path=root/'config.yaml';path.write_text(json.dumps(config))
        with (root/'cpa.log').open('w') as log:
            proc=subprocess.Popen([str(binary),'--config',str(path),'--local-model'],cwd=root,stdout=log,stderr=subprocess.STDOUT)
            try:
                deadline=time.monotonic()+20
                while True:
                    try:
                        models=json.loads(http(port,'/v1/models'))
                        if 'designer' in [m['id'] for m in models['data']]: break
                    except (OSError,ValueError): pass
                    if proc.poll() is not None or time.monotonic()>deadline: raise AssertionError('role not registered')
                    time.sleep(.1)
                body={'model':'designer','messages':[{'role':'user','content':'hello'}],'reasoning_effort':'low'}
                result=json.loads(http(port,'/v1/chat/completions',body)); assert result['choices'][0]['message']['content']=='g',result
                assert set(a for a,_ in Mock.calls[:2])=={'a','b'} and len(Mock.calls)==3 and Mock.calls[-1][0]=='g',Mock.calls
                assert [b.get('reasoning_effort') for _,b in Mock.calls]==['high','high','xhigh'],Mock.calls
                result=json.loads(http(port,'/v1/chat/completions',body));assert result['choices'][0]['message']['content']=='g',result
                Mock.failures.clear();time.sleep(1.1)
                result=json.loads(http(port,'/v1/chat/completions',body));assert result['choices'][0]['message']['content']=='a',result
                interactive={**body,'model':'interactive'};http(port,'/v1/chat/completions',interactive)
                assert Mock.calls[-1][1]['reasoning_effort']=='low',Mock.calls[-1]
                stream=http(port,'/v1/chat/completions',{**body,'stream':True});assert b'data:' in stream and b'[DONE]' in stream,stream
                claude=http(port,'/v1/messages',{'model':'designer','max_tokens':32,'messages':body['messages'],'output_config':{'effort':'low'}})
                assert json.loads(claude)['content'][0]['text']=='a',claude
                responses=http(port,'/v1/responses',{'model':'designer','input':'hello','stream':True})
                assert b'response.completed' in responses,responses
                Mock.failures.update({'a':503,'b':503})
                before=len(Mock.calls)
                streamed=http(port,'/v1/chat/completions',{**body,'stream':True})
                assert Mock.calls[-1][0]=='g' and set(a for a,_ in Mock.calls[before:])=={'a','b','g'},Mock.calls[before:]
                Mock.failures.clear();time.sleep(1.1);Mock.partial=True
                before=len(Mock.calls)
                partial=http(port,'/v1/chat/completions',{**body,'stream':True})
                assert [a for a,_ in Mock.calls[before:]]==['a'] and b'error' in partial, (Mock.calls[before:],partial)
                Mock.partial=False
                count=json.loads(http(port,'/v1/messages/count_tokens',{'model':'designer','messages':body['messages']}))
                assert count['input_tokens']>0,count
                claude_role=json.loads(http(port,'/v1/messages',{'model':'claude-worker','max_tokens':32,'messages':body['messages']},extra_headers={'X-Claude-Code-Session-Id':'original-desktop-chat'}))
                assert claude_role['content'][0]['text']=='claude',claude_role
                deadline=time.monotonic()+3
                while True:
                    st=json.loads(http(port,'/v0/management/plugins/kpatdev-quota-router/status',management=True))
                    if st['counters']['observed']>0:break
                    if time.monotonic()>deadline:raise AssertionError('usage observation not delivered')
                    time.sleep(.1)
                assert st['sessions']==0,st
                print('PASS: actual CPA role catalog, cross-provider/account fallback, effort override, recovery, Chat Completions/Claude/Responses and streaming',flush=True)
                if preview := os.environ.get('ROLE_PREVIEW_FILE'):
                    done=Path(preview+'.done');done.unlink(missing_ok=True)
                    Path(preview).write_text(json.dumps({'port':port}))
                    deadline=time.monotonic()+180
                    while not done.exists() and time.monotonic()<deadline: time.sleep(.2)
            except BaseException:
                print("mock calls:", Mock.calls, file=sys.stderr)
                try: print("role status:", http(port,"/v0/management/plugins/kpatdev-quota-router/roles",management=True),file=sys.stderr)
                except Exception: pass
                log.flush();print((root/'cpa.log').read_text(errors='replace')[-16000:],file=sys.stderr);raise
            finally:stop(proc)
    mock.shutdown()

if __name__=='__main__': main()
