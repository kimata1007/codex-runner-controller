// Keep transport text separate from the presentation model.
export function parseOutput(source) {
  const blocks = [];
  const lines = source.replace(/\r\n/g, '\n').split('\n');
  let prose = [];
  const flush = () => {
    if (prose.length) blocks.push({type: 'markdown', source: prose.join('\n')});
    prose = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const fence = /^ {0,3}(`{3,}|~{3,})\s*([^\s`~]*)[^\n]*$/.exec(lines[i]);
    if (!fence) { prose.push(lines[i]); continue; }
    flush();
    const body = [];
    const end = new RegExp('^ {0,3}' + fence[1][0] + '{' + fence[1].length + ',}\\s*$');
    while (++i < lines.length && !end.test(lines[i])) body.push(lines[i]);
    const language = fence[2].toLowerCase();
    const type = ['mermaid', 'chart', 'svg', 'diff', 'math', 'latex'].includes(language) ? language : 'code';
    blocks.push({type, language, source: body.join('\n'), complete: i < lines.length});
  }
  flush();
  return {blocks};
}
