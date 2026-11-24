/* Shared UI: theme toggle. Calculator logic lives in each tool's calc.js. */
(function(){"use strict";
  var root=document.documentElement;
  function stored(){try{return localStorage.getItem("omsme-theme");}catch(e){return null;}}
  function current(){var t=root.getAttribute("data-theme");if(t)return t;return (window.matchMedia&&window.matchMedia("(prefers-color-scheme: dark)").matches)?"dark":"light";}
  function paint(b){if(!b)return;var d=current()==="dark";b.textContent=d?"☀ Light":"☾ Dark";b.setAttribute("aria-label",d?"Switch to light theme":"Switch to dark theme");}
  var t=document.querySelector("[data-themer]");
  if(t){paint(t);t.addEventListener("click",function(){var n=current()==="dark"?"light":"dark";root.setAttribute("data-theme",n);try{localStorage.setItem("omsme-theme",n);}catch(e){}paint(t);});
    if(window.matchMedia){window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change",function(){if(!stored())paint(t);});}}
})();
