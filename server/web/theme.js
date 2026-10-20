(function () {
  try {
    var t = localStorage.getItem("bm.theme");
    if (t && t !== "classic") document.documentElement.setAttribute("data-theme", t);
  } catch (e) {}
})();
