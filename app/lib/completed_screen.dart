import 'package:flutter/material.dart';

class CompletedScreen extends StatelessWidget {
  const CompletedScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Completed'),
      ),
      body: const Center(
        child: Text('Completed downloads will go here'),
      ),
    );
  }
}
