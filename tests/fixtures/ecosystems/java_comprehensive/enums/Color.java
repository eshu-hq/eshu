package comprehensive.enums;

/**
 * Enum with methods and fields.
 */
public enum Color {
    RED(255, 0, 0),
    GREEN(0, 255, 0),
    BLUE(0, 0, 255);

    private final int r;
    private final int g;
    private final int b;

    Color(int r, int g, int b) {
        this.r = r;
        this.g = g;
        this.b = b;
    }

    public String hex() {
        return String.format("#%02x%02x%02x", r, g, b);
    }

    public int brightness() {
        return (r + g + b) / 3;
    }
}
