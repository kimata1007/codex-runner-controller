const state = {
  apiURL: window.location.origin,
  conversationID: sessionStorage.getItem('codex.conversationID') || '',
  threadID: sessionStorage.getItem('codex.threadID') || '',
};

const mode = document.querySelector('#mode');
const repositoryFields = document.querySelector('#repository-fields');
const revisionFields = document.querySelector('#revision-fields');
const repositoryURL = document.querySelector('#repository-url');
const repositoryRevision = document.querySelector('#repository-revision');
const status = document.querySelector('#status');
const conversationID = document.querySelector('#conversation-id');
const messages = document.querySelector('#messages');
const composer = document.querySelector('#message-form');
const messageInput = document.querySelector('#message');
const sendButton = composer.querySelector('button');

sessionStorage.removeItem('codex.apiURL');
sessionStorage.removeItem('codex.apiToken');
conversationID.textContent = state.conversationID || '会話未作成';

function updateModeFields() {
  const implement = mode.value === 'implement';
  repositoryFields.classList.toggle('hidden', !implement);
  revisionFields.classList.toggle('hidden', !implement);
}

function setStatus(text) { status.textContent = text; }

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set('Content-Type', 'application/json');
  const response = await fetch(`${state.apiURL}${path}`, {...options, headers});
  if (!response.ok) throw new Error(`${response.status}: ${await response.text()}`);
  return response.json();
}

function addMessage(role, content) {
  const item = document.createElement('li');
  item.className = `message ${role}`;
  item.textContent = content;
  messages.append(item);
  item.scrollIntoView({block: 'nearest'});
  return item;
}

function addApproval(runID, payload) {
  const item = document.createElement('li');
  item.className = 'approval';
  const label = document.createElement('p');
  label.textContent = payload.method || 'Codexが承認を求めています';
  const detail = document.createElement('pre');
  detail.textContent = JSON.stringify(payload.params || {}, null, 2);
  const actions = document.createElement('div');
  actions.className = 'approval-actions';
  for (const [labelText, decision] of [['許可', 'accept'], ['拒否', 'decline']]) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = labelText;
    button.addEventListener('click', async () => {
      button.disabled = true;
      try {
        await request(`/v1/approvals/${runID}/${payload.requestId}`, {
          method: 'POST',
          body: JSON.stringify({response: {decision}}),
        });
        item.remove();
        setStatus('承認結果を送信しました');
      } catch (error) {
        button.disabled = false;
        setStatus(`承認エラー: ${error.message}`);
      }
    });
    actions.append(button);
  }
  item.append(label, detail, actions);
  messages.append(item);
}

async function createConversation() {
  const body = {mode: mode.value};
  if (mode.value === 'implement') {
    body.repository = {url: repositoryURL.value.trim(), revision: repositoryRevision.value.trim()};
  }
  const conversation = await request('/v1/conversations', {method: 'POST', body: JSON.stringify(body)});
  state.conversationID = conversation.id;
  state.threadID = '';
  sessionStorage.setItem('codex.conversationID', state.conversationID);
  sessionStorage.removeItem('codex.threadID');
  conversationID.textContent = state.conversationID;
  messages.replaceChildren();
  setStatus('会話を作成しました');
}

async function loadHistory() {
  if (!state.conversationID) return;
  try {
    const result = await request(`/v1/conversations/${state.conversationID}/messages`);
    messages.replaceChildren();
    for (const message of result.messages || []) addMessage(message.role === 'user' ? 'user' : 'assistant', message.content);
    setStatus('会話を復元しました');
  } catch (error) {
    setStatus(`履歴復元エラー: ${error.message}`);
  }
}

async function streamEvents(runID, assistantItem) {
  const response = await fetch(`${state.apiURL}/v1/runs/${runID}/events`);
  if (!response.ok || !response.body) throw new Error(`event stream: ${response.status}`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const {value, done} = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), {stream: !done});
    const chunks = buffer.split('\n\n');
    buffer = chunks.pop() || '';
    for (const chunk of chunks) {
      const eventLine = chunk.split('\n').find((line) => line.startsWith('event: '));
      const eventType = eventLine ? eventLine.slice(7) : '';
      const data = chunk.split('\n').find((line) => line.startsWith('data: '));
      if (data) {
        try {
          const payload = JSON.parse(data.slice(6));
          if (eventType === 'approval/requested') {
            addApproval(runID, payload);
            setStatus('承認待ちです');
            continue;
          }
          const text = payload.text || payload.delta || payload.content || '';
          if (text) assistantItem.textContent += text;
        } catch (_) {
          // Event payloads that are not user-visible JSON are ignored by the UI.
        }
      }
      if (eventType === 'turn/completed' || eventType === 'turn/failed') {
        await reader.cancel();
        return eventType === 'turn/completed';
      }
    }
    if (done) return false;
  }
}

async function sendMessage(event) {
  event.preventDefault();
  const content = messageInput.value.trim();
  if (!content) return;
  if (!state.conversationID) await createConversation();
  addMessage('user', content);
  messageInput.value = '';
  sendButton.disabled = true;
  setStatus('Codexを起動しています…');
  try {
    const result = await request(`/v1/conversations/${state.conversationID}/messages`, {
      method: 'POST',
      body: JSON.stringify({content, clientMessageId: crypto.randomUUID(), threadId: state.threadID}),
    });
    if (!result.run) { setStatus('メッセージを保存しました'); return; }
    state.threadID = result.run.thread_id || state.threadID;
    if (state.threadID) sessionStorage.setItem('codex.threadID', state.threadID);
    const assistant = addMessage('assistant', '');
    setStatus('Codexが応答中…');
    const completed = await streamEvents(result.run.id, assistant);
    setStatus(completed ? '完了' : '応答が終了しました');
  } catch (error) {
    setStatus(`エラー: ${error.message}`);
  } finally {
    sendButton.disabled = false;
  }
}

document.querySelector('#new-conversation').addEventListener('click', createConversation);
mode.addEventListener('change', updateModeFields);
updateModeFields();
composer.addEventListener('submit', sendMessage);
if ('serviceWorker' in navigator) navigator.serviceWorker.register('sw.js');
fetch('/healthz').then((response) => {
  if (!response.ok) throw new Error(`${response.status}`);
  setStatus('接続済み');
}).catch((error) => setStatus(`接続エラー: ${error.message}`));
loadHistory();
