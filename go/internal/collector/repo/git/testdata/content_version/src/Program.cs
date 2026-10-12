using System;
using Comprehensive.Models;
using Comprehensive.Interfaces;
using Comprehensive.Services;

namespace Comprehensive
{
    class Program
    {
        static void Main(string[] args)
        {
            var person = new Person("Alice", 30);
            var employee = new Employee("Bob", 25, "Engineering");

            Console.WriteLine(person.Greet());
            Console.WriteLine(employee.Greet());

            IService service = new GreetingService();
            Console.WriteLine(service.Execute("World"));
        }

        static T Max<T>(T a, T b) where T : IComparable<T>
        {
            return a.CompareTo(b) > 0 ? a : b;
        }
    }
}
