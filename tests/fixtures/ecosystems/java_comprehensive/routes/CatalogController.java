package comprehensive.routes;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/catalog")
public class CatalogController {
    @GetMapping("/items/{id}")
    public String show(@PathVariable String id) {
        return id;
    }

    @PostMapping("/items")
    public String create() {
        return "created";
    }
}
