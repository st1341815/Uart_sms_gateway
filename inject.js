(() => {
  const badgeRe = /^【([^】]+)】/;

  function splitBadgeInTextNode(el){
    if(!el || el.nodeType !== 1) return;

    // only handle leaf-ish nodes with a single text child
    if(el.childNodes.length === 1 && el.firstChild.nodeType === 3){
      const txt = el.textContent || '';
      const m = txt.match(badgeRe);
      if(m){
        const name = m[1];
        const rest = txt.replace(badgeRe,'');
        const span = document.createElement('span');
        span.className = 'sms-device-badge';
        span.textContent = name;
        el.textContent = '';
        el.appendChild(span);
        el.appendChild(document.createTextNode(rest));
      }
    }
  }

  function scan(root){
    const treeWalker = document.createTreeWalker(root || document.body, NodeFilter.SHOW_ELEMENT);
    let n;
    while((n = treeWalker.nextNode())){
      // quick filter: only elements that contain our marker
      if((n.textContent || '').startsWith('【')) splitBadgeInTextNode(n);
    }
  }

  // initial
  const ready = () => scan(document.body);
  if(document.readyState === 'loading') document.addEventListener('DOMContentLoaded', ready);
  else ready();

  // observe SPA updates
  const mo = new MutationObserver((muts) => {
    for(const mut of muts){
      for(const node of mut.addedNodes){
        if(node.nodeType === 1) scan(node);
      }
    }
  });
  mo.observe(document.documentElement, {childList:true, subtree:true});
})();
