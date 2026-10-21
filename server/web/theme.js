(function () {
  try {
    var root = document.documentElement;
    var b = localStorage.getItem("bm.base");
    var k = localStorage.getItem("bm.key");
    var old = localStorage.getItem("bm.theme");
    if (old !== null) {
      var map = {
        classic: ["classic", "yellow"], slate: ["slate", "blue"], lagoon: ["slate", "cyan"], frost: ["slate", "blue"],
        dusk: ["dusk", "purple"], graphite: ["graphite", "yellow"], ember: ["graphite", "coral"], moss: ["moss", "green"],
        sunset: ["dusk", "coral"], candy: ["slate", "pink"], neon: ["night", "pink"], jewel: ["night", "green"],
        terminal: ["night", "green"], volt: ["night", "lime"], paper: ["classic", "yellow"]
      };
      var m = map[old] || ["classic", "yellow"];
      if (!b && !k) {
        b = m[0];
        k = m[1];
        localStorage.setItem("bm.base", b);
        localStorage.setItem("bm.key", k);
      }
      localStorage.removeItem("bm.theme");
    }
    if (b && b !== "classic") root.setAttribute("data-base", b);
    if (k && k !== "yellow") root.setAttribute("data-key", k);
  } catch (e) {}
})();
