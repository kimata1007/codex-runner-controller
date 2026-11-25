import {parseOutput} from './output-parser.js';

const el = (tag, text) => {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  return node;
};
const svgNS = 'http://www.w3.org/2000/svg';
let sequence = 0;

function code(block) {
  const pre = el('pre');
  const body = el('code', block.source);
  if (block.language) body.className = 'language-' + block.language;
  pre.append(body);
  if (block.type === 'diff') {
    body.replaceChildren(...block.source.split('\n').map(line => {
      const row = el('span', line + '\n');
      row.className = line.startsWith('+') ? 'diff-add' : line.startsWith('-') ? 'diff-remove' : '';
      return row;
    }));
  } else if (globalThis.hljs && block.language && globalThis.hljs.getLanguage(block.language)) {
    globalThis.hljs.highlightElement(body);
  }
  return pre;
}

function markdown(source) {
  const node = el('div');
  if (!globalThis.marked || !globalThis.DOMPurify) {
    node.append(el('p', source));
    return node;
  }
  node.innerHTML = globalThis.DOMPurify.sanitize(globalThis.marked.parse(source, {gfm: true}), {
    USE_PROFILES: {html: true}, FORBID_TAGS: ['img', 'form', 'style', 'iframe', 'object', 'embed'],
    FORBID_ATTR: ['style', 'src', 'srcset']
  });
  for (const input of node.querySelectorAll('input')) {
    if (input.type !== 'checkbox') input.remove();
    else input.disabled = true;
  }
  if (globalThis.renderMathInElement) {
    globalThis.renderMathInElement(node, {delimiters: [
      {left: '$$', right: '$$', display: true},
      {left: '\\[', right: '\\]', display: true},
      {left: '\\(', right: '\\)', display: false},
      {left: '$', right: '$', display: false}
    ], throwOnError: false, trust: false});
  }
  return node;
}

function safeSvg(source) {
  // Display as an image, never inject AI-provided SVG into the document.
  if (!globalThis.DOMPurify) throw Error('SVG sanitizer unavailable');
  const clean = globalThis.DOMPurify.sanitize(source, {
    USE_PROFILES: {svg: true, svgFilters: true},
    FORBID_TAGS: ['foreignObject', 'style', 'script', 'image', 'use', 'a', 'animate', 'set'],
    FORBID_ATTR: ['href', 'xlink:href', 'style']
  });
  const doc = new DOMParser().parseFromString(clean, 'image/svg+xml');
  if (doc.documentElement.localName !== 'svg' || doc.documentElement.namespaceURI !== svgNS) throw Error('Invalid SVG');
  const image = el('img');
  image.alt = 'SVG diagram';
  image.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(clean);
  return image;
}

function chart(source) {
  const data = JSON.parse(source);
  if (!['bar', 'line'].includes(data.type) || !Array.isArray(data.labels) ||
      !Array.isArray(data.values) || !data.labels.length || data.labels.length > 100 ||
      data.labels.length !== data.values.length || !data.labels.every(v => typeof v === 'string') ||
      !data.values.every(v => typeof v === 'number' && Number.isFinite(v))) throw Error('Invalid chart');
  if (!globalThis.Chart) throw Error('Chart library unavailable');
  const canvas = el('canvas');
  canvas.setAttribute('role', 'img');
  canvas.setAttribute('aria-label', data.labels.map((label, i) => label + ': ' + data.values[i]).join(', '));
  const wrapper = el('div');
  wrapper.className = 'output-chart';
  wrapper.append(canvas);
  new globalThis.Chart(canvas, {type: data.type, data: {labels: data.labels, datasets: [
    {label: typeof data.title === 'string' ? data.title : '', data: data.values, borderColor: '#426b91', backgroundColor: '#7aa4c8'}
  ]}, options: {responsive: true, animation: false}});
  return wrapper;
}

function renderBlock(block) {
  if (block.type === 'markdown') return markdown(block.source);
  if (!block.complete || block.type === 'code' || block.type === 'diff') return code(block);
  try {
    if (block.type === 'svg') return safeSvg(block.source);
    if (block.type === 'chart') return chart(block.source);
    if (block.type === 'math' || block.type === 'latex') {
      const node = el('div');
      if (!globalThis.katex) return code(block);
      globalThis.katex.render(block.source, node, {displayMode: true, trust: false});
      return node;
    }
    if (block.type === 'mermaid' && globalThis.mermaid && globalThis.DOMPurify) {
      const node = el('div');
      node.append(code(block));
      globalThis.mermaid.initialize({startOnLoad: false, securityLevel: 'strict', flowchart: {htmlLabels: false}, maxTextSize: 50000});
      globalThis.mermaid.render('output-diagram-' + (++sequence), block.source)
        .then(({svg}) => { if (node.isConnected) node.replaceChildren(safeSvg(svg)); })
        .catch(() => {});
      return node;
    }
  } catch (_) { /* A malformed block must never hide its source. */ }
  return code(block);
}

// Reuse completed blocks as streaming text grows; preserve original source.
export class OutputRenderer {
  constructor(container) {
    this.container = container;
    this.blocks = [];
  }
  render(source) {
    const blocks = parseOutput(source).blocks;
    let prefix = 0;
    while (prefix < blocks.length && prefix < this.blocks.length &&
      JSON.stringify(blocks[prefix]) === JSON.stringify(this.blocks[prefix])) prefix++;
    while (this.container.children.length > prefix) {
      const node = this.container.lastElementChild;
      for (const canvas of node.querySelectorAll('canvas')) globalThis.Chart?.getChart(canvas)?.destroy();
      node.remove();
    }
    for (const block of blocks.slice(prefix)) {
      const wrapper = el('section');
      wrapper.className = 'output-block';
      try { wrapper.append(renderBlock(block)); } catch (_) { wrapper.append(code(block)); }
      this.container.append(wrapper);
    }
    this.blocks = blocks;
  }
}
