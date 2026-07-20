# A plain service object (PORO) with intra-repo calls, so the corpus emits
# CALLS edges alongside the INHERITS chain. Not a controller; not rooted.
class WidgetService
  def all
    fetch_all
  end

  def find(id)
    fetch_one(id)
  end

  def fetch_all
    []
  end

  def fetch_one(id)
    { id: id }
  end
end
