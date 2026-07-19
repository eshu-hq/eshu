package comprehensive.routes;

import jakarta.ws.rs.GET;
import jakarta.ws.rs.Path;

@Path("/widgets")
public class WidgetResource {
    @GET
    @Path("/{id}")
    public String get(String id) {
        return id;
    }
}
