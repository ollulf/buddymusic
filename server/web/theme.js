(function () {
  try {
    var t = localStorage.getItem("bm.theme");
    if (t && t !== "classic") document.documentElement.setAttribute("data-theme", t);
    var b = localStorage.getItem("bm.base");
    var k = localStorage.getItem("bm.key");
    if (b) document.documentElement.setAttribute("data-base", b);
    if (k) document.documentElement.setAttribute("data-key", k);
  } catch (e) {}
})();
