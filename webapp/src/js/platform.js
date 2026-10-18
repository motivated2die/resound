// What kind of device this is, decided before the first paint (loaded in the
// head, not deferred: the page must not flash the other layout).
//   - ios: iPhones and iPads (class "ios" on <html>). They never hide the
//     status bar, not even when installed, so the top bar moves below it
//     there (css/styles.css). iPadOS reports itself as a Mac with touch.
//   - mobile: phones and tablets, which go fullscreen by default
//     (fullscreen.js); desktop browsers don't. Android, iOS and iPadOS, or
//     any device whose main input is touch without hover.
window.platform = (() => {
    const ua = navigator.userAgent;
    const ios = /iPhone|iPad|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
    const mobile = ios || /Android/.test(ua) || window.matchMedia('(hover: none) and (pointer: coarse)').matches;
    if (ios) document.documentElement.classList.add('ios');
    return { ios, mobile };
})();
