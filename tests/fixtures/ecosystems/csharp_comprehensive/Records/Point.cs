namespace Comprehensive.Records
{
    // Record type
    public record Point(double X, double Y)
    {
        public double DistanceTo(Point other)
        {
            var dx = X - other.X;
            var dy = Y - other.Y;
            return System.Math.Sqrt(dx * dx + dy * dy);
        }
    }

    // Record struct
    public record struct Color(byte R, byte G, byte B)
    {
        public string Hex => $"#{R:X2}{G:X2}{B:X2}";
    }
}
