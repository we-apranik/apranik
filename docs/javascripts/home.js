(function () {
  function initBackToTop() {
    var button = document.querySelector(".ap-home .totop");
    if (!button) return;
    function update() { button.hidden = window.scrollY < 600; }
    window.addEventListener("scroll", update, { passive: true });
    update();
    button.addEventListener("click", function () {
      var calm = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      window.scrollTo({ top: 0, behavior: calm ? "auto" : "smooth" });
      document.querySelector(".md-header a, .md-header button").focus({ preventScroll: true });
    });
  }

  var loopTimer = null;

  function init() {
    initBackToTop();
    if (loopTimer) { clearInterval(loopTimer); loopTimer = null; }
    var wrap = document.querySelector(".ap-home .loopwrap");
    var steps = document.querySelectorAll(".ap-home [data-step]");
    var infoBoxes = document.querySelectorAll(".ap-home .loopinfo");
    if (!wrap || !steps.length) return;
    var stepCount = document.querySelectorAll(".ap-home .loop li.st").length;
    var current = 0;

    function pick(index) {
      current = index;
      var source = null;
      steps.forEach(function (x) {
        var on = x.dataset.step === String(index);
        x.classList.toggle("sel", on);
        if (on && !source) source = x;
      });
      document.querySelectorAll(".ap-home .lseg").forEach(function (segment, segmentIndex) {
        segment.classList.toggle("flow", segmentIndex === (index + 6) % 7);
      });
      var accent = source.classList.contains("ap") ? "var(--ap-green)"
        : source.classList.contains("ppl") ? "var(--ap-blue)" : "var(--ap-red)";
      infoBoxes.forEach(function (box) {
        box.style.borderLeftColor = accent;
        box.innerHTML = "<b>" + source.dataset.t + "</b><span>" + source.dataset.d + "</span>";
      });
    }

    pick(0);

    var reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    var visible = false;

    function startTimer() {
      if (loopTimer) clearInterval(loopTimer);
      loopTimer = null;
      if (reduceMotion) return;
      loopTimer = setInterval(function () {
        if (visible) pick((current + 1) % stepCount);
      }, 3000);
    }

    function choose(index) {
      pick(index);
      startTimer();
    }

    steps.forEach(function (x) {
      if (x.tagName === "LI") {
        x.tabIndex = 0;
        x.setAttribute("role", "button");
      }
      x.addEventListener("click", function () { choose(Number(x.dataset.step)); });
      x.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); choose(Number(x.dataset.step)); }
      });
    });

    if (reduceMotion) {
      pick(2);
      return;
    }

    new IntersectionObserver(function (entries) {
      visible = entries[0].isIntersecting;
    }).observe(wrap);
    startTimer();
  }

  if (typeof document$ !== "undefined") {
    document$.subscribe(init);
  } else if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
