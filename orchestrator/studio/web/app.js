// wave Studio — vanilla ES module, zero build step.
//
// Single-page app that hash-routes between project list and project
// detail. Talks to /api/* using a token stored in the studio_token
// cookie. The opening URL embeds ?t=<token>; we capture it on first
// load, set the cookie, and strip it from the URL.

(function bootstrapToken() {
  const url = new URL(location.href);
  const t = url.searchParams.get('t');
  if (t) {
    document.cookie = `studio_token=${t}; path=/; max-age=2592000; SameSite=Lax`;
    url.searchParams.delete('t');
    history.replaceState({}, '', url.toString());
  }
})();

// ───── api wrapper ───────────────────────────────────────────────────
const api = {
  async req(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    const r = await fetch(path, opts);
    if (!r.ok) {
      const text = await r.text();
      throw new Error(`${r.status}: ${text.trim()}`);
    }
    if (r.status === 204) return null;
    const ct = r.headers.get('Content-Type') || '';
    if (ct.includes('application/json')) return r.json();
    return r.text();
  },
  listProjects: () => api.req('GET', '/api/projects'),
  addProject: (path, configFile) =>
    api.req('POST', '/api/projects', { path, config_file: configFile }),
  scaffoldProject: (name, kind, parentDir) =>
    api.req('POST', '/api/projects/scaffold', { name, kind, parent_dir: parentDir }),
  removeProject: (id) => api.req('DELETE', `/api/projects/${id}`),
  start: (id) => api.req('POST', `/api/projects/${id}/start`),
  stop: (id) => api.req('POST', `/api/projects/${id}/stop`),
  restart: (id) => api.req('POST', `/api/projects/${id}/restart`),
  status: (id) => api.req('GET', `/api/projects/${id}/status`),
  routes: (id) => api.req('GET', `/api/projects/${id}/routes`),
  testRoute: (id, payload) => api.req('POST', `/api/projects/${id}/test-route`, payload),
  metrics: (id) => api.req('GET', `/api/projects/${id}/metrics`),
};

// ───── helpers ───────────────────────────────────────────────────────
function el(tag, props = {}, ...children) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === 'className') e.className = v;
    else if (k === 'onclick') e.onclick = v;
    else if (k.startsWith('on') && typeof v === 'function')
      e.addEventListener(k.slice(2), v);
    else if (k === 'html') e.innerHTML = v;
    else if (v != null) e.setAttribute(k, v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    e.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return e;
}

function pill(status) {
  return el('span', { className: `pill ${status}` }, status);
}

function clear(node) { while (node.firstChild) node.removeChild(node.firstChild); }

// ───── routing ───────────────────────────────────────────────────────
const root = document.getElementById('app');
const nav = document.getElementById('nav');

function navigate(hash) {
  if (location.hash !== hash) location.hash = hash;
  else render();
}
window.addEventListener('hashchange', render);

function parseRoute() {
  const h = location.hash.replace(/^#/, '') || '/';
  const m = h.match(/^\/projects\/([^/]+)(?:\/(\w+))?$/);
  if (m) return { name: 'project', id: m[1], tab: m[2] || 'overview' };
  return { name: 'list' };
}

async function render() {
  const r = parseRoute();
  clear(root);
  clear(nav);
  nav.appendChild(el('a', {
    href: '#/',
    className: r.name === 'list' ? 'active' : '',
  }, 'Projects'));
  if (r.name === 'project') {
    nav.appendChild(el('a', {
      href: `#/projects/${r.id}`,
      className: 'active',
    }, 'Project'));
  }
  try {
    if (r.name === 'list') await renderList();
    else await renderProject(r.id, r.tab);
  } catch (e) {
    root.appendChild(el('div', { className: 'error' }, String(e)));
  }
}

// ───── project list ─────────────────────────────────────────────────
async function renderList() {
  const data = await api.listProjects();
  const projects = data.projects || [];

  const header = el('div', { className: 'row', style: 'margin-bottom:16px' },
    el('h1', { style: 'flex:1; margin:0' }, 'Projects'),
    el('button', { className: 'shrink', onclick: openAddModal }, 'Add Project'),
    el('button', { className: 'shrink primary', onclick: openScaffoldModal }, 'Scaffold'),
  );
  root.appendChild(header);

  if (projects.length === 0) {
    root.appendChild(el('div', { className: 'empty' },
      'No projects yet. Click ', el('strong', {}, 'Add Project'),
      ' to register an existing one or ', el('strong', {}, 'Scaffold'),
      ' to create a new one.'));
    return;
  }

  const grid = el('div', { className: 'grid cards' });
  for (const p of projects) grid.appendChild(projectCard(p));
  root.appendChild(grid);
}

function projectCard(p) {
  const card = el('div', { className: 'card' });
  card.appendChild(el('h3', {}, p.name, ' ', pill(p.status)));
  card.appendChild(el('div', { className: 'path' }, p.path));
  if (p.uptime > 0) {
    card.appendChild(el('div', { className: 'muted', style: 'font-size:12px;margin-top:4px' },
      `pid ${p.pid} · uptime ${p.uptime}s · restarts ${p.restarts}`));
  }
  const acts = el('div', { className: 'actions' },
    el('button', { onclick: () => navigate(`#/projects/${p.id}`) }, 'Open'),
    p.status === 'running' || p.status === 'starting'
      ? el('button', { onclick: async () => { await api.stop(p.id); render(); } }, 'Stop')
      : el('button', { className: 'primary', onclick: async () => { await api.start(p.id); render(); } }, 'Start'),
    el('button', { onclick: async () => { await api.restart(p.id); render(); } }, 'Restart'),
    el('button', {
      className: 'danger',
      onclick: async () => {
        if (!confirm(`Unregister ${p.name}? Files on disk are not deleted.`)) return;
        await api.removeProject(p.id); render();
      },
    }, 'Unregister'),
  );
  card.appendChild(acts);
  return card;
}

function openAddModal() { renderModal('Add existing project', addProjectForm()); }
function openScaffoldModal() { renderModal('Scaffold new project', scaffoldForm()); }

function renderModal(title, body) {
  const bg = el('div', { className: 'modal-bg', onclick: (e) => { if (e.target === bg) bg.remove(); } });
  const m = el('div', { className: 'modal' });
  m.appendChild(el('h2', {}, title));
  m.appendChild(body);
  bg.appendChild(m);
  document.body.appendChild(bg);
  m._close = () => bg.remove();
  return m;
}

function addProjectForm() {
  const path = el('input', { placeholder: '/absolute/path/to/project' });
  const cf = el('input', { placeholder: 'server.yaml', value: 'server.yaml' });
  const err = el('div', { className: 'error' });
  const form = el('div', { className: 'spaced' },
    el('div', { className: 'field' }, el('label', {}, 'Project path'), path),
    el('div', { className: 'field' }, el('label', {}, 'Config file'), cf),
    err,
    el('div', { className: 'row' },
      el('button', {
        className: 'primary',
        onclick: async () => {
          err.textContent = '';
          try {
            await api.addProject(path.value.trim(), cf.value.trim() || 'server.yaml');
            form.closest('.modal')._close();
            render();
          } catch (e) { err.textContent = String(e); }
        },
      }, 'Add'),
      el('button', { onclick: () => form.closest('.modal')._close() }, 'Cancel'),
    ),
  );
  return form;
}

function scaffoldForm() {
  const name = el('input', { placeholder: 'my-api' });
  const kind = el('select', {});
  ['api', 'spa', 'internal-tool', 'plugin-starter', 'streaming', 'oidc-api', 'graphql'].forEach((k) =>
    kind.appendChild(el('option', { value: k }, k)));
  const parent = el('input', { placeholder: '/absolute/parent/dir' });
  const err = el('div', { className: 'error' });
  const form = el('div', { className: 'spaced' },
    el('div', { className: 'field' }, el('label', {}, 'Name'), name),
    el('div', { className: 'field' }, el('label', {}, 'Template'), kind),
    el('div', { className: 'field' }, el('label', {}, 'Parent directory'), parent),
    err,
    el('div', { className: 'row' },
      el('button', {
        className: 'primary',
        onclick: async () => {
          err.textContent = '';
          try {
            await api.scaffoldProject(name.value.trim(), kind.value, parent.value.trim());
            form.closest('.modal')._close();
            render();
          } catch (e) { err.textContent = String(e); }
        },
      }, 'Scaffold'),
      el('button', { onclick: () => form.closest('.modal')._close() }, 'Cancel'),
    ),
  );
  return form;
}

// ───── project detail ──────────────────────────────────────────────
async function renderProject(id, tab) {
  const all = await api.listProjects();
  const p = (all.projects || []).find((x) => x.id === id);
  if (!p) {
    root.appendChild(el('div', {}, 'Project not found.'));
    return;
  }
  const back = el('a', { href: '#/', className: 'muted' }, '← all projects');
  root.appendChild(back);
  root.appendChild(el('h1', { style: 'margin:8px 0 4px' }, p.name, ' ', pill(p.status)));
  root.appendChild(el('div', { className: 'path' }, p.path));

  const tabs = el('div', { className: 'tabs', style: 'margin-top:16px' });
  const tabContainer = el('div');
  const TABS = ['overview', 'routes', 'tester', 'logs', 'metrics'];
  for (const t of TABS) {
    tabs.appendChild(el('button', {
      className: t === tab ? 'active' : '',
      onclick: () => navigate(`#/projects/${id}/${t}`),
    }, t));
  }
  root.appendChild(tabs);
  root.appendChild(tabContainer);

  switch (tab) {
    case 'overview': await tabOverview(tabContainer, p); break;
    case 'routes': await tabRoutes(tabContainer, p); break;
    case 'tester': await tabTester(tabContainer, p); break;
    case 'logs': await tabLogs(tabContainer, p); break;
    case 'metrics': await tabMetrics(tabContainer, p); break;
  }
}

async function tabOverview(container, p) {
  const status = await api.status(p.id);
  container.appendChild(el('div', { className: 'card spaced' },
    el('div', { className: 'kv' },
      el('span', { className: 'k' }, 'Status'), el('span', {}, pill(status.status)),
      el('span', { className: 'k' }, 'PID'), el('span', {}, String(status.pid || '—')),
      el('span', { className: 'k' }, 'Uptime'), el('span', {}, status.uptime ? `${status.uptime}s` : '—'),
      el('span', { className: 'k' }, 'Restarts'), el('span', {}, String(status.restarts || 0)),
      el('span', { className: 'k' }, 'Path'), el('span', { className: 'path' }, p.path),
      el('span', { className: 'k' }, 'Config'), el('span', { className: 'path' }, p.config_file || 'server.yaml'),
    ),
    el('div', { className: 'actions' },
      status.status === 'running' || status.status === 'starting'
        ? el('button', { onclick: async () => { await api.stop(p.id); render(); } }, 'Stop')
        : el('button', { className: 'primary', onclick: async () => { await api.start(p.id); render(); } }, 'Start'),
      el('button', { onclick: async () => { await api.restart(p.id); render(); } }, 'Restart'),
    ),
  ));
}

async function tabRoutes(container, p) {
  const data = await api.routes(p.id);
  const tbl = el('table');
  tbl.appendChild(el('thead', {}, el('tr', {},
    el('th', {}, 'Method'), el('th', {}, 'Path'), el('th', {}, 'Type'), el('th', {}, 'Description'))));
  const body = el('tbody');
  for (const r of data.routes || []) {
    const tr = el('tr', {
      className: 'clickable',
      onclick: () => {
        sessionStorage.setItem('tester_prefill', JSON.stringify({ method: r.method, path: r.path }));
        navigate(`#/projects/${p.id}/tester`);
      },
    },
      el('td', {}, r.method || (r.methods || ['GET']).join(',')),
      el('td', { className: 'path' }, r.path),
      el('td', {}, r.type || ''),
      el('td', { className: 'muted' }, r.description || ''),
    );
    body.appendChild(tr);
  }
  tbl.appendChild(body);
  if ((data.routes || []).length === 0) {
    container.appendChild(el('div', { className: 'empty' }, 'No routes parsed.'));
  } else {
    container.appendChild(el('div', { className: 'card' }, tbl));
  }
  container.appendChild(el('div', { className: 'muted', style: 'margin-top:8px;font-size:12px' },
    `Server address: ${data.host}:${data.port}`));
}

async function tabTester(container, p) {
  const prefill = (() => {
    const s = sessionStorage.getItem('tester_prefill');
    if (!s) return { method: 'GET', path: '/' };
    sessionStorage.removeItem('tester_prefill');
    try { return JSON.parse(s); } catch { return { method: 'GET', path: '/' }; }
  })();

  const method = el('select', {});
  ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'].forEach((m) =>
    method.appendChild(el('option', { value: m, selected: m === (prefill.method || 'GET') ? '' : null }, m)));
  const path = el('input', { value: prefill.path || '/' });
  const headers = el('textarea', { rows: '3', placeholder: 'Header-Name: value (one per line)' });
  const body = el('textarea', { rows: '6', placeholder: '{ }' });
  const out = el('div');

  const submit = el('button', { className: 'primary' }, 'Send');
  submit.onclick = async () => {
    out.replaceChildren(el('div', { className: 'muted' }, 'sending…'));
    const hdrs = {};
    for (const ln of headers.value.split('\n')) {
      const i = ln.indexOf(':');
      if (i > 0) hdrs[ln.slice(0, i).trim()] = ln.slice(i + 1).trim();
    }
    try {
      const r = await api.testRoute(p.id, {
        method: method.value, path: path.value, headers: hdrs, body: body.value,
      });
      const hdrLines = Object.entries(r.headers || {})
        .map(([k, v]) => `${k}: ${v.join(', ')}`).join('\n');
      out.replaceChildren(
        el('div', { className: 'card spaced' },
          el('div', {}, 'Status: ', el('strong', {}, String(r.status)),
            ' · ', el('span', { className: 'muted' }, `${r.duration_ms}ms`)),
          el('div', {},
            el('div', { className: 'muted', style: 'font-size:12px;margin-bottom:4px' }, 'Headers'),
            el('pre', { className: 'metrics' }, hdrLines)),
          el('div', {},
            el('div', { className: 'muted', style: 'font-size:12px;margin-bottom:4px' }, 'Body'),
            el('pre', { className: 'metrics' }, prettyJSON(r.body))),
        ));
    } catch (e) {
      out.replaceChildren(el('div', { className: 'error' }, String(e)));
    }
  };

  container.appendChild(el('div', { className: 'card spaced' },
    el('div', { className: 'row' },
      el('div', { className: 'shrink', style: 'min-width:120px' }, method),
      path),
    el('div', { className: 'field' }, el('label', {}, 'Headers'), headers),
    el('div', { className: 'field' }, el('label', {}, 'Body'), body),
    el('div', {}, submit),
  ));
  container.appendChild(out);
}

function prettyJSON(s) {
  if (!s) return '';
  try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; }
}

async function tabLogs(container, p) {
  const log = el('pre', { className: 'log' });
  let paused = false;
  const ctrls = el('div', { className: 'actions', style: 'margin-bottom:8px' });
  const pauseBtn = el('button', {}, 'Pause');
  pauseBtn.onclick = () => { paused = !paused; pauseBtn.textContent = paused ? 'Resume' : 'Pause'; };
  ctrls.appendChild(pauseBtn);
  ctrls.appendChild(el('button', { onclick: () => { log.textContent = ''; } }, 'Clear'));
  container.appendChild(ctrls);
  container.appendChild(log);

  const es = new EventSource(`/api/projects/${p.id}/logs`);
  es.onmessage = (ev) => {
    if (paused) return;
    log.textContent += ev.data + '\n';
    log.scrollTop = log.scrollHeight;
  };
  es.onerror = () => {
    log.textContent += '\n[disconnected]\n';
    es.close();
  };
  // close stream when navigating away
  window.addEventListener('hashchange', () => es.close(), { once: true });
}

async function tabMetrics(container, p) {
  const summary = el('div', { className: 'card', style: 'margin-bottom:12px' });
  const raw = el('pre', { className: 'metrics' });
  container.appendChild(summary);
  container.appendChild(raw);

  async function refresh() {
    try {
      const text = await api.metrics(p.id);
      raw.textContent = text;
      summary.replaceChildren(metricsTable(text));
    } catch (e) {
      summary.replaceChildren(el('div', { className: 'error' }, String(e)));
      raw.textContent = '';
    }
  }
  await refresh();
  const id = setInterval(refresh, 5000);
  window.addEventListener('hashchange', () => clearInterval(id), { once: true });
}

function metricsTable(text) {
  const rows = [];
  const re = /^(wave_(?:http_requests_total|plugin_calls_total)\{[^}]*\})\s+([0-9.eE+-]+)/gm;
  let m;
  while ((m = re.exec(text))) rows.push([m[1], m[2]]);
  if (rows.length === 0) return el('div', { className: 'muted' }, 'No counters yet.');
  const t = el('table');
  t.appendChild(el('thead', {}, el('tr', {},
    el('th', {}, 'Counter'), el('th', {}, 'Value'))));
  const tb = el('tbody');
  for (const [k, v] of rows.slice(0, 50)) {
    tb.appendChild(el('tr', {}, el('td', { className: 'path' }, k), el('td', {}, v)));
  }
  t.appendChild(tb);
  return t;
}

render();
