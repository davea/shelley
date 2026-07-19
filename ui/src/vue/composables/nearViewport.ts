// Sticky "has this element ever been near the viewport?" tracking, built on a
// single shared IntersectionObserver.
//
// Used to defer expensive hydration (e.g. @pierre/diffs FileDiff instances)
// until the user can actually see the result. In a huge conversation the
// message list contains hundreds of diffs; hydrating them all up front puts
// ~200k elements of shadow DOM into the document, which makes every viewport
// resize re-lay-out for seconds. Deferring keeps the typical case (reading
// the recent tail of the conversation) small.
//
// The flag is sticky: once an element has been near the viewport it stays
// "near" forever, so hydrated content is never torn down by scrolling away.
import { onBeforeUnmount, ref, watch, type Ref } from "vue";

// Start hydrating one viewport-height before the element scrolls into view.
const ROOT_MARGIN = "100%";

type Callback = () => void;
let sharedObserver: IntersectionObserver | null = null;
const callbacks = new WeakMap<Element, Callback>();

function observer(): IntersectionObserver {
  if (!sharedObserver) {
    sharedObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          const cb = callbacks.get(entry.target);
          if (cb) {
            callbacks.delete(entry.target);
            sharedObserver?.unobserve(entry.target);
            cb();
          }
        }
      },
      { rootMargin: ROOT_MARGIN },
    );
  }
  return sharedObserver;
}

// Returns a ref that flips to true once `el` comes within one viewport of
// view (and stays true). If IntersectionObserver is unavailable (jsdom), the
// ref is true immediately.
export function useNearViewport(el: Ref<Element | null>): Ref<boolean> {
  const near = ref(typeof IntersectionObserver === "undefined");

  watch(
    el,
    (element, prev) => {
      if (prev) {
        callbacks.delete(prev);
        sharedObserver?.unobserve(prev);
      }
      if (element && !near.value) {
        callbacks.set(element, () => {
          near.value = true;
        });
        observer().observe(element);
      }
    },
    { immediate: true, flush: "post" },
  );

  onBeforeUnmount(() => {
    const element = el.value;
    if (element) {
      callbacks.delete(element);
      sharedObserver?.unobserve(element);
    }
  });

  return near;
}
