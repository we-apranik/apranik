(function () {
  function init() {
    var items = document.querySelectorAll(".ap-home .loop li.st");
    var info = document.getElementById("loopinfo");
    if (!items.length || !info) return;

    function pick(li) {
      items.forEach(function (x) { x.classList.remove("sel"); });
      li.classList.add("sel");
      var ap = li.classList.contains("ap");
      info.style.borderLeftColor = ap ? "var(--ap-green)" : "var(--ap-ink2)";
      info.innerHTML = "<b>" + li.dataset.t + "</b><span>" + li.dataset.d + "</span>";
    }

    items.forEach(function (li) {
      li.addEventListener("click", function () { pick(li); });
      li.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); pick(li); }
      });
    });

    pick(items[2]);
  }

  if (typeof document$ !== "undefined") {
    document$.subscribe(init);
  } else if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
