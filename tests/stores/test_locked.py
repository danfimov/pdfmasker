import itertools
import threading

from pdfmasker import LockedSubstitutionStore


def test_locked_store_stays_consistent_under_threads():
    store = LockedSubstitutionStore()
    values = itertools.count()
    keys = [f"k{index}" for index in range(200)]
    seen = []

    def worker():
        seen.append({key: store.get_or_assign(key, lambda: f"v{next(values)}") for key in keys})

    threads = [threading.Thread(target=worker) for _ in range(8)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert all(mapping == seen[0] for mapping in seen)
    assert len(set(seen[0].values())) == len(seen[0])
    assert dict(store.items()) == seen[0]
