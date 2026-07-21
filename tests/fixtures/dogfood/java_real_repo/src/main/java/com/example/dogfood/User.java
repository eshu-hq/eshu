// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Shape informed by public patterns in Spring Boot-style services
// (a plain model class under a controller/service/model package layout).
// Synthetic, hand-authored for this fixture; no external repo content is
// vendored here.

package com.example.dogfood;

public class User {
    private final String id;
    private final String name;
    private final String email;

    public User(String id, String name, String email) {
        this.id = id;
        this.name = name;
        this.email = email;
    }

    public String getId() {
        return id;
    }

    public String getName() {
        return name;
    }

    public String getEmail() {
        return email;
    }

    @Override
    public String toString() {
        return "User{" + "id='" + id + '\'' + ", name='" + name + '\'' + '}';
    }
}
