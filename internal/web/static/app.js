// app.js - sshops Web UI
const API = '';

// Tab switching
document.querySelectorAll('.tab').forEach(tab => {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
    tab.classList.add('active');
    document.getElementById('tab-' + tab.dataset.tab).classList.add('active');
  });
});

// Load hosts on page load
async function loadHosts() {
  try {
    const resp = await fetch(API + '/api/hosts');
    const hosts = await resp.json();
    const hostList = document.getElementById('hostList');
    const execHost = document.getElementById('execHost');
    
    if (!hosts || hosts.length === 0) {
      hostList.innerHTML = '<div class="host-item empty">暂无主机</div>';
      return;
    }
    
    hostList.innerHTML = hosts.map(h => `
      <div class="host-item" data-name="${h.name}">
        <span class="host-dot"></span>
        <span>${h.name}</span>
      </div>
    `).join('');
    
    execHost.innerHTML = '<option value="">-- 选择主机 --</option>' + 
      hosts.map(h => `<option value="${h.name}">${h.name}</option>`).join('');
    
    // Host item click
    hostList.querySelectorAll('.host-item:not(.empty)').forEach(item => {
      item.addEventListener('click', () => {
        hostList.querySelectorAll('.host-item').forEach(i => i.classList.remove('selected'));
        item.classList.add('selected');
        execHost.value = item.dataset.name;
      });
    });
  } catch (e) { console.error('Failed to load hosts:', e); }
}

// Exec command
document.getElementById('execBtn').addEventListener('click', async () => {
  const host = document.getElementById('execHost').value;
  const cmd = document.getElementById('commandInput').value;
  const output = document.getElementById('execOutput');
  
  if (!host || !cmd) { alert('请选择主机并输入命令'); return; }
  
  output.textContent = '执行中...\n';
  
  try {
    const resp = await fetch(API + '/api/exec', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host, command: cmd })
    });
    const result = await resp.json();
    output.textContent = result.output || JSON.stringify(result, null, 2);
  } catch (e) { output.textContent = '错误: ' + e.message; }
});

document.getElementById('clearBtn').addEventListener('click', () => {
  document.getElementById('commandInput').value = '';
  document.getElementById('execOutput').textContent = '';
});

// Load playbooks
async function loadPlaybooks() {
  try {
    const resp = await fetch(API + '/api/playbooks');
    const data = await resp.json();
    const list = document.getElementById('playbookList');
    
    if (!data.playbooks || data.playbooks.length === 0) {
      list.innerHTML = '<div class="empty">暂无 Playbook</div>';
      return;
    }
    
    list.innerHTML = data.playbooks.map(p => `
      <div class="playbook-item">
        <div class="playbook-info">
          <span class="playbook-name">${p.name}</span>
          <span class="playbook-meta">${p.hosts} | ${p.tasks} tasks</span>
        </div>
        <button class="btn btn-secondary" onclick="runPlaybook('${p.name}')">运行</button>
      </div>
    `).join('');
  } catch (e) { console.error('Failed to load playbooks:', e); }
}

async function runPlaybook(name) {
  const vars = prompt('输入变量 (格式: key=value,key=value)，可留空');
  const output = document.getElementById('playbookOutput');
  output.style.display = 'block';
  output.textContent = '执行中...\n';
  
  try {
    const resp = await fetch(API + '/api/playbooks/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, vars: {} })
    });
    const result = await resp.json();
    output.textContent = result.report || JSON.stringify(result, null, 2);
  } catch (e) { output.textContent = '错误: ' + e.message; }
}

// Load audit logs
async function loadAudit() {
  try {
    const resp = await fetch(API + '/api/logs?limit=50');
    const data = await resp.json();
    const tbody = document.getElementById('auditBody');
    
    if (!data.logs || data.logs.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5">暂无记录</td></tr>';
      return;
    }
    
    tbody.innerHTML = data.logs.map(l => `
      <tr>
        <td>${l.time || ''}</td>
        <td>${l.host || ''}</td>
        <td>${l.command || ''}</td>
        <td>${l.exit_code || 0}</td>
        <td>${l.duration || '0s'}</td>
      </tr>
    `).join('');
  } catch (e) { console.error('Failed to load audit:', e); }
}

document.getElementById('refreshAuditBtn')?.addEventListener('click', loadAudit);

// Init
loadHosts();
loadPlaybooks();
loadAudit();
