using System;
using System.Runtime.Serialization;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Mvc;
using Microsoft.Extensions.Hosting;
using Xunit;

namespace DeadCodeFixture;

public interface IJob
{
    void Run();
}

public sealed class ReportJob : IJob
{
    public ReportJob()
    {
    }

    public void Run()
    {
        DirectlyUsedHelper();
    }

    private static void DirectlyUsedHelper()
    {
        Console.WriteLine("used");
    }

    private static void UnusedCleanupCandidate()
    {
        Console.WriteLine("unused");
    }
}

public sealed class PublicController
{
    [HttpGet]
    public string Get() => "ok";

    private string InternalHelper() => "private";
}

public sealed class Worker : BackgroundService
{
    protected override Task ExecuteAsync(CancellationToken stoppingToken)
    {
        Console.WriteLine("worker");
        return Task.CompletedTask;
    }
}

public sealed class FixtureTests
{
    [Fact]
    public void ExercisedByTestRunner()
    {
        DirectlyUsedTestHelper();
    }

    private void DirectlyUsedTestHelper()
    {
        Console.WriteLine("test helper");
    }
}

public sealed class SerializationHooks
{
    [OnDeserialized]
    private void Restore(StreamingContext context)
    {
        Console.WriteLine("restore");
    }
}

public static class GeneratedFile
{
    public static void GeneratedExcludedHelper() => Console.WriteLine("generated");
}

public static class Program
{
    public const string DynamicActionName = "Run";

    public static void Main()
    {
        IJob job = new ReportJob();
        job.Run();
        new PublicController().Get();
    }
}
