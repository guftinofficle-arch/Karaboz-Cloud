import React,{useEffect,useState} from 'react';
import{createRoot}from'react-dom/client';
import'./style.css';

const API=(import.meta.env.VITE_API_BASE||'http://localhost:8080').replace(/\/$/,'');
async function call(path,opts={}) {
 const headers={};
 if(opts.token) headers.Authorization='Bearer '+opts.token;
 if(opts.key) headers['X-Karaboz-Key']=opts.key;
 if(opts.body!==undefined) headers['Content-Type']='application/json';
 const response=await fetch(API+path,{method:opts.method||'GET',headers,body:opts.body===undefined?undefined:JSON.stringify(opts.body)});
 const data=await response.json().catch(()=>({}));
 if(!response.ok) throw new Error(data.error||'Request failed');
 return data;
}
function App(){
 const [token,setToken]=useState(localStorage.getItem('kbc_session')||'');
 const [mode,setMode]=useState('login');
 const [name,setName]=useState('');const [email,setEmail]=useState('');const [password,setPassword]=useState('');
 const [projects,setProjects]=useState([]);const [selected,setSelected]=useState('');
 const [projectName,setProjectName]=useState('');const [keyName,setKeyName]=useState('Primary app key');
 const [permission,setPermission]=useState('read_write');const [newKey,setNewKey]=useState('');
 const [message,setMessage]=useState('');const [busy,setBusy]=useState(false);
 async function loadProjects(t=token){const d=await call('/v1/projects',{token:t});setProjects(d.projects||[]);if(!selected&&d.projects?.length)setSelected(d.projects[0].id);}
 useEffect(()=>{if(token)loadProjects().catch(e=>{setMessage(e.message);setToken('');localStorage.removeItem('kbc_session');});},[]);
 async function auth(e){e.preventDefault();setBusy(true);setMessage('');try{const body=mode==='register'?{display_name:name,email,password}:{email,password};const d=await call('/v1/auth/'+(mode==='register'?'register':'login'),{method:'POST',body});setToken(d.token);localStorage.setItem('kbc_session',d.token);await loadProjects(d.token);setMessage('Signed in successfully.');}catch(e){setMessage(e.message);}finally{setBusy(false);}}
 async function addProject(e){e.preventDefault();setBusy(true);try{const d=await call('/v1/projects',{token,method:'POST',body:{name:projectName}});setProjectName('');await loadProjects();setSelected(d.project.id);setMessage('Project created.');}catch(e){setMessage(e.message);}finally{setBusy(false);}}
 async function addKey(e){e.preventDefault();setBusy(true);setNewKey('');try{const d=await call('/v1/projects/'+selected+'/keys',{token,method:'POST',body:{name:keyName,permission}});setNewKey(d.api_key);setMessage('Copy and save this key now. It is only shown once.');}catch(e){setMessage(e.message);}finally{setBusy(false);}}
 function logout(){localStorage.removeItem('kbc_session');setToken('');setProjects([]);setSelected('');setNewKey('');}
 const project=projects.find(p=>p.id===selected);
 return <div className="shell">
  <header className="top"><div className="brand"><div className="mark">K</div><div><b>Karaboz Cloud</b><small>Storage infrastructure for your apps</small></div></div>{token&&<button className="quiet" onClick={logout}>Sign out</button>}</header>
  {!token?<main className="auth card"><span className="eyebrow">DEVELOPER CONSOLE</span><h1>{mode==='register'?'Create your cloud account':'Welcome back'}</h1><p className="muted">Manage projects and issue scoped API keys for your apps.</p><form onSubmit={auth}>{mode==='register'&&<label>Display name<input required minLength="2" value={name} onChange={e=>setName(e.target.value)} placeholder="Your name"/></label>}<label>Email<input type="email" required value={email} onChange={e=>setEmail(e.target.value)} placeholder="you@example.com"/></label><label>Password<input type="password" required minLength="10" value={password} onChange={e=>setPassword(e.target.value)} placeholder="At least 10 characters"/></label><button className="primary" disabled={busy}>{busy?'Please wait…':mode==='register'?'Create account':'Sign in'}</button></form><p className="switch">{mode==='register'?'Already registered?':'New to Karaboz Cloud?'} <button className="link" onClick={()=>setMode(mode==='register'?'login':'register')}>{mode==='register'?'Sign in':'Create account'}</button></p></main>
  :<main className="layout"><aside className="card side"><span className="eyebrow">WORKSPACE</span><h2>Projects</h2>{projects.map(p=><button key={p.id} className={'project '+(p.id===selected?'active':'')} onClick={()=>{setSelected(p.id);setNewKey('');}}><b>{p.name}</b><small>{p.storage_used_bytes||0} bytes used</small></button>)}<form className="new-project" onSubmit={addProject}><label>New project<input required maxLength="80" value={projectName} onChange={e=>setProjectName(e.target.value)} placeholder="e.g. Guftin production"/></label><button className="secondary" disabled={busy}>Create project</button></form></aside>
  <section className="content"><div className="welcome"><div><span className="eyebrow">DEVELOPER WORKSPACE</span><h1>{project?.name||'Your projects'}</h1><p className="muted">One shared API for your own services and external integrations.</p></div><span className="status">API workspace</span></div>
  {project?<><div className="stats"><div className="card stat"><small>Storage used</small><strong>{project.storage_used_bytes||0} B</strong></div><div className="card stat"><small>Buckets</small><strong>{project.bucket_count||0}</strong></div><div className="card stat"><small>Project quota</small><strong>{project.storage_limit_bytes||0} B</strong></div></div>
  <div className="card panel"><span className="eyebrow">ACCESS CONTROL</span><h2>Create an API key</h2><p className="muted">Generate a project-scoped key for Guftin, your own service or another app.</p><form className="keyform" onSubmit={addKey}><label>Key name<input required maxLength="60" value={keyName} onChange={e=>setKeyName(e.target.value)}/></label><label>Permission<select value={permission} onChange={e=>setPermission(e.target.value)}><option value="read_write">Read and write</option><option value="read_only">Read only</option></select></label><button className="primary" disabled={busy}>Generate API key</button></form>{newKey&&<div className="keybox"><b>Your new API key</b><code>{newKey}</code><button className="secondary" onClick={()=>navigator.clipboard?.writeText(newKey).then(()=>setMessage('API key copied.'))}>Copy key</button><p>Save it now. The complete key cannot be retrieved again.</p></div>}</div>
  <div className="card panel"><span className="eyebrow">QUICK START</span><h2>Connect an application</h2><p className="muted">Send your project key in the X-Karaboz-Key header. Never expose a write-enabled key in public browser code.</p><pre>{'curl -X POST '+API+'/v1/buckets \\\n  -H "X-Karaboz-Key: YOUR_PROJECT_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d \'{"name":"uploads"}\''}</pre><a href="https://github.com/guftinofficle-arch/Karaboz-Cloud/blob/main/docs/API.md" target="_blank" rel="noreferrer">Read API documentation ↗</a></div></>:<div className="card panel"><h2>Create your first project</h2><p className="muted">A project represents one app or environment and owns its buckets, files and API keys.</p></div>}
  </section></main>}
  {message&&<div className="toast" role="status">{message}<button onClick={()=>setMessage('')}>×</button></div>}<footer>Karaboz Cloud · Developer preview · Configure HTTPS and secure your keys before launch.</footer>
 </div>;
}
createRoot(document.getElementById('root')).render(<App/>);
