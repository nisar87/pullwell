"""Quick end-to-end check of ordinary downloads through the running program against fixtures.py.
   python smoke.py [download_dir]   -- the folder is set as the download folder
   PORT (default 9197) and HEADER (default X-Pullwell) can be set in the environment."""
import json, urllib.request, hashlib, time, os, sys, tempfile
DIR=sys.argv[1] if len(sys.argv)>1 else os.path.join(tempfile.gettempdir(),"fxdl")
PORT=os.environ.get("PORT","9197"); HEADER=os.environ.get("HEADER","X-Pullwell")
API="http://127.0.0.1:%s/api/downloads"%PORT
def call(d):
    r=urllib.request.Request(API,json.dumps(d).encode(),{"Content-Type":"application/json",HEADER:"1"}); 
    try: return json.load(urllib.request.urlopen(r))
    except urllib.error.HTTPError as e: return {"http":e.code,"body":e.read().decode()[:200]}
def items(): return json.load(urllib.request.urlopen(API))["items"]
def wait(id,t=60):
    for _ in range(t*4):
        v=[x for x in items() if x["id"]==id]
        if v and v[0]["state"] in("done","error"): return v[0]
        time.sleep(.25)
def sha(p): return hashlib.sha256(open(p,"rb").read()).hexdigest()
exp=lambda n: urllib.request.urlopen("http://127.0.0.1:8765/sha256/"+n).read().decode()
call({"action":"config","config":{**json.load(urllib.request.urlopen(API))["config"],"dir":DIR}})
os.makedirs(DIR,exist_ok=True)
for path,n in [("file","5MB"),("flaky","5MB"),("status503","2MB"),("status403","1MB"),("redirect","1MB"),("norange","3MB"),("slow","1MB")]:
    r=call({"action":"add","url":f"http://127.0.0.1:8765/{path}/{n}"+("?bps=2000000" if path=="slow" else ""),"force":True})
    if "http" in r: print(path,"ADD FAIL",r); continue
    v=wait(r["added"][0]["id"])
    ok = v["state"]=="done" and sha(v["path"])==exp(n)
    print(path,n,v["state"],"hash ok" if ok else "HASH/STATE BAD "+str(v.get("error")))
for q,h in [("hls/master.m3u8",0),("hls/master.m3u8",720)]:
    r=call({"action":"probe","url":"http://127.0.0.1:8765/static/"+q,"height":h}); print("probe",q,h,r.get("hls",{}).get("heights"), str(r)[:120] if "hls" not in r else "")
