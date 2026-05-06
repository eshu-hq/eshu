namespace Comprehensive.Models
{
    public class Employee : Person
    {
        public string Department { get; }

        public Employee(string name, int age, string department)
            : base(name, age)
        {
            Department = department;
        }

        public override string Greet()
        {
            return $"Hi, I'm {Name} from {Department}";
        }
    }
}
